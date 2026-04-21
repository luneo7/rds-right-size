package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// BulkInstanceInfo holds hardware specs and pricing extracted from the public AWS bulk pricing JSON.
type BulkInstanceInfo struct {
	Price            float64
	VCPUs            int64
	MemoryGiB        int64
	MaxBandwidthMbps *int64
}

// Bulk pricing JSON types
type bulkPricingResponse struct {
	Products map[string]bulkProduct `json:"products"`
	Terms    struct {
		OnDemand map[string]map[string]bulkTerm `json:"OnDemand"`
	} `json:"terms"`
}

type bulkProduct struct {
	Attributes struct {
		InstanceType       string `json:"instanceType"`
		DatabaseEngine     string `json:"databaseEngine"`
		DeploymentOption   string `json:"deploymentOption"`
		Storage            string `json:"storage"`
		VCPU               string `json:"vcpu"`
		Memory             string `json:"memory"`
		NetworkPerformance string `json:"networkPerformance"`
	} `json:"attributes"`
}

type bulkTerm struct {
	PriceDimensions map[string]struct {
		PricePerUnit map[string]string `json:"pricePerUnit"`
	} `json:"priceDimensions"`
}

// regionIndexResponse represents the public AWS pricing region index.
type regionIndexResponse struct {
	Regions map[string]struct {
		RegionCode string `json:"regionCode"`
	} `json:"regions"`
}

// bandwidthRegex extracts numeric bandwidth from strings like "Up to 10 Gigabit", "25 Gigabit"
var bandwidthRegex = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*Gigabit`)

// govOrLocalZoneRegex matches AWS GovCloud and local zone region codes.
var govOrLocalZoneRegex = regexp.MustCompile(`^(us-gov-|.*-lax-|.*-wl1-)`)

// FetchRegionList fetches the public AWS pricing region index for RDS and returns
// a sorted list of region codes, excluding GovCloud and local zone regions.
// This requires no AWS credentials.
func FetchRegionList(ctx context.Context) ([]string, error) {
	url := "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonRDS/current/region_index.json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch region index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("region index returned HTTP %d", resp.StatusCode)
	}

	var index regionIndexResponse
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("failed to parse region index: %w", err)
	}

	var regions []string
	for _, info := range index.Regions {
		code := info.RegionCode
		if code == "" {
			continue
		}
		// Skip GovCloud and local zone regions
		if govOrLocalZoneRegex.MatchString(code) {
			continue
		}
		regions = append(regions, code)
	}

	sort.Strings(regions)
	return regions, nil
}

// FetchBulkInstanceData downloads the public AWS bulk pricing JSON for the given
// region and engine, and extracts hardware specs + on-demand pricing for all
// matching Aurora instance types.
// This requires no AWS credentials.
func FetchBulkInstanceData(ctx context.Context, engine string, region string) (map[string]BulkInstanceInfo, error) {
	url := fmt.Sprintf("https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonRDS/current/%s/index.json", region)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download bulk pricing for %s: %w", region, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bulk pricing download for %s returned HTTP %d", region, resp.StatusCode)
	}

	// Decode directly from the HTTP response body (streams, avoids full buffering)
	var bulk bulkPricingResponse
	if err := json.NewDecoder(resp.Body).Decode(&bulk); err != nil {
		return nil, fmt.Errorf("failed to parse bulk pricing JSON for %s: %w", region, err)
	}

	databaseEngine := engineToPricingEngine(engine)

	// Phase 1: Find matching SKUs and extract hardware specs
	type skuInfo struct {
		instanceType string
		vcpus        int64
		memoryGiB    int64
		maxBandwidth *int64
	}
	skuMap := make(map[string]skuInfo)

	for sku, product := range bulk.Products {
		attrs := product.Attributes

		if !strings.EqualFold(attrs.DatabaseEngine, databaseEngine) {
			continue
		}
		if attrs.DeploymentOption != "Single-AZ" {
			continue
		}
		if attrs.Storage != "EBS Only" {
			continue
		}
		if !strings.HasPrefix(attrs.InstanceType, "db.") {
			continue
		}

		vcpus := parseVCPU(attrs.VCPU)
		memGiB := parseMemory(attrs.Memory)
		bandwidth := parseBandwidth(attrs.NetworkPerformance)

		skuMap[sku] = skuInfo{
			instanceType: attrs.InstanceType,
			vcpus:        vcpus,
			memoryGiB:    memGiB,
			maxBandwidth: bandwidth,
		}
	}

	// Phase 2: Extract on-demand prices from terms for matched SKUs
	result := make(map[string]BulkInstanceInfo)
	for sku, info := range skuMap {
		terms, ok := bulk.Terms.OnDemand[sku]
		if !ok {
			continue
		}

		var price float64
		for _, term := range terms {
			for _, dim := range term.PriceDimensions {
				if usd, ok := dim.PricePerUnit["USD"]; ok {
					p, err := strconv.ParseFloat(usd, 64)
					if err == nil && p > 0 {
						price = p
						break
					}
				}
			}
			if price > 0 {
				break
			}
		}

		if price > 0 {
			result[info.instanceType] = BulkInstanceInfo{
				Price:            price,
				VCPUs:            info.vcpus,
				MemoryGiB:        info.memoryGiB,
				MaxBandwidthMbps: info.maxBandwidth,
			}
		}
	}

	return result, nil
}

// parseVCPU parses a vcpu string like "2" to int64.
func parseVCPU(s string) int64 {
	s = strings.TrimSpace(s)
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseMemory parses a memory string like "16 GiB" to int64 (in GiB).
func parseMemory(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " GiB")
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

// parseBandwidth extracts bandwidth in Mbps from a network performance string.
// Examples: "Up to 10 Gigabit" -> 10000, "25 Gigabit" -> 25000
func parseBandwidth(performance string) *int64 {
	matches := bandwidthRegex.FindStringSubmatch(performance)
	if len(matches) < 2 {
		return nil
	}

	gbps, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return nil
	}

	mbps := int64(gbps * 1000)
	return &mbps
}

// engineToPricingEngine maps our engine names to the AWS Pricing JSON's databaseEngine attribute values.
func engineToPricingEngine(engine string) string {
	switch engine {
	case "aurora-mysql":
		return "Aurora MySQL"
	case "aurora-postgresql":
		return "Aurora PostgreSQL"
	default:
		return "Aurora MySQL"
	}
}
