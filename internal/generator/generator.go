package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsRds "github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/luneo7/rds-right-size/internal/rds-right-size/types"
	"github.com/luneo7/rds-right-size/internal/util"
)

// GenerateOptions configures the instance types generation.
type GenerateOptions struct {
	Engine        string // "both", "aurora-mysql", or "aurora-postgresql"
	Region        string // AWS home region for auth/SDK config
	TargetRegions string // Comma-separated target regions, or "all" for all enabled regions
	Output        string // Output file path
	OnStatus      func(status string) // Optional status callback for progress
}

// orderableClassInfo holds per-instance-class data collected from DescribeOrderableDBInstanceOptions.
type orderableClassInfo struct {
	engineVersions []string // all supported engine versions (for min version calc)
}

// GenerateInstanceTypes builds an instance types JSON file by:
// 1. Discovering target regions from the public AWS pricing region index
// 2. Downloading bulk pricing JSON per region for hardware specs, pricing, and availability
// 3. Collecting engine version support from RDS DescribeOrderableDBInstanceOptions (home region)
// 4. Computing max connections per engine
// 5. Building up/down linked lists within each instance family
// 6. Writing the result to a JSON file
//
// When engine is "both", it runs the pipeline for aurora-mysql and aurora-postgresql
// separately, then merges the results using engine-prefixed keys (e.g., "aurora-mysql:db.r6g.large").
// Single-engine generation preserves plain keys for backward compatibility.
func GenerateInstanceTypes(ctx context.Context, cfg aws.Config, opts GenerateOptions) error {
	status := func(msg string) {
		if opts.OnStatus != nil {
			opts.OnStatus(msg)
		}
	}

	engine := opts.Engine
	if engine == "" {
		engine = "both"
	}

	// Discover target regions
	targetRegions, err := resolveTargetRegions(ctx, opts.TargetRegions, opts.Region, status)
	if err != nil {
		return fmt.Errorf("failed to resolve target regions: %w", err)
	}
	status(fmt.Sprintf("Target regions (%d): %s", len(targetRegions), strings.Join(targetRegions, ", ")))

	var instanceTypes types.InstanceTypes

	if engine == "both" {
		engines := []string{"aurora-mysql", "aurora-postgresql"}
		instanceTypes = make(types.InstanceTypes)

		for _, eng := range engines {
			status(fmt.Sprintf("--- Generating for %s ---", eng))
			engineTypes, err := generateForEngine(ctx, cfg, eng, opts.Region, targetRegions, status)
			if err != nil {
				return fmt.Errorf("failed generating for %s: %w", eng, err)
			}

			// Merge with engine-prefixed keys
			for cls, props := range engineTypes {
				prefixedKey := eng + ":" + cls
				// Prefix Up/Down pointers
				if props.Up != nil {
					prefixed := eng + ":" + *props.Up
					props.Up = &prefixed
				}
				if props.Down != nil {
					prefixed := eng + ":" + *props.Down
					props.Down = &prefixed
				}
				instanceTypes[prefixedKey] = props
			}

			status(fmt.Sprintf("Merged %d instance types for %s", len(engineTypes), eng))
		}
	} else {
		instanceTypes, err = generateForEngine(ctx, cfg, engine, opts.Region, targetRegions, status)
		if err != nil {
			return err
		}
	}

	// Write JSON
	status("Writing JSON file...")
	output := opts.Output
	if output == "" {
		output = "aurora_instance_types.json"
	}

	data, err := json.MarshalIndent(instanceTypes, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile(output, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", output, err)
	}

	status(fmt.Sprintf("Done! Written %d instance types to %s", len(instanceTypes), output))
	return nil
}

// resolveTargetRegions determines which AWS regions to include in the generated JSON.
// If targetRegions is "all" or empty, it fetches the public AWS pricing region index.
// Otherwise, it parses the comma-separated list.
// warn is called (non-nil) when a non-fatal fallback occurs.
func resolveTargetRegions(ctx context.Context, targetRegions string, homeRegion string, warn func(string)) ([]string, error) {
	if targetRegions != "" && targetRegions != "all" {
		// Parse comma-separated list
		parts := strings.Split(targetRegions, ",")
		var regions []string
		for _, r := range parts {
			r = strings.TrimSpace(r)
			if r != "" {
				regions = append(regions, r)
			}
		}
		return regions, nil
	}

	// Discover all regions from the public pricing region index (no credentials needed)
	regions, err := FetchRegionList(ctx)
	if err != nil {
		// Fallback: if region index fails, use just the home region
		if homeRegion != "" {
			warn(fmt.Sprintf("Warning: region list fetch failed (%v); falling back to home region %s only", err, homeRegion))
			return []string{homeRegion}, nil
		}
		return nil, fmt.Errorf("region index fetch failed and no home region specified: %w", err)
	}

	return regions, nil
}

// generateForEngine runs the full generation pipeline for a single engine and returns
// an InstanceTypes map with plain (non-prefixed) keys.
func generateForEngine(ctx context.Context, cfg aws.Config, engine string, homeRegion string, targetRegions []string, status func(string)) (types.InstanceTypes, error) {
	// Run two independent tasks in parallel:
	// 1. Fetch bulk JSON data for all target regions (hardware specs + pricing + availability)
	// 2. Fetch engine version info from DescribeOrderableDBInstanceOptions (home region)

	type bulkResult struct {
		regionData map[string]map[string]BulkInstanceInfo // region -> instance type -> info
		err        error
	}

	type orderableResult struct {
		classInfo map[string]*orderableClassInfo
		err       error
	}

	bulkCh := make(chan bulkResult, 1)
	orderableCh := make(chan orderableResult, 1)

	// Task 1: Fetch bulk JSON for all regions
	go func() {
		status(fmt.Sprintf("[%s] Fetching pricing data across %d regions...", engine, len(targetRegions)))
		data, err := fetchMultiRegionData(ctx, engine, targetRegions, status)
		bulkCh <- bulkResult{regionData: data, err: err}
	}()

	// Task 2: Fetch engine version info from DescribeOrderableDBInstanceOptions
	go func() {
		status(fmt.Sprintf("[%s] Collecting engine version support from DescribeOrderableDBInstanceOptions...", engine))
		info, err := listOrderableClassInfo(ctx, cfg, engine)
		orderableCh <- orderableResult{classInfo: info, err: err}
	}()

	// Wait for both to complete
	bulkRes := <-bulkCh
	orderableRes := <-orderableCh

	if bulkRes.err != nil {
		status(fmt.Sprintf("[%s] Warning: multi-region bulk fetch had errors: %v", engine, bulkRes.err))
	}
	if orderableRes.err != nil {
		status(fmt.Sprintf("[%s] Warning: DescribeOrderableDBInstanceOptions failed: %v", engine, orderableRes.err))
		// Non-fatal: we just won't have MinEngineVersion data
	}

	regionData := bulkRes.regionData
	classInfo := orderableRes.classInfo

	// Discover the union of all instance classes seen across all regions
	allClassesSet := make(map[string]bool)
	for _, regionInstances := range regionData {
		for cls := range regionInstances {
			allClassesSet[cls] = true
		}
	}

	var allClasses []string
	for cls := range allClassesSet {
		allClasses = append(allClasses, cls)
	}
	sort.Strings(allClasses)

	status(fmt.Sprintf("[%s] Found %d unique instance classes across %d regions", engine, len(allClasses), len(regionData)))

	if len(allClasses) == 0 {
		return nil, fmt.Errorf("no instance classes found for engine %s across target regions", engine)
	}

	// Build instance properties
	status(fmt.Sprintf("[%s] Building instance properties...", engine))
	instanceTypes := make(types.InstanceTypes)

	for _, cls := range allClasses {
		// Take hardware specs from the first region that has this class
		var vcpu, mem int64
		var maxBandwidth *int64
		for _, regionInstances := range regionData {
			if info, ok := regionInstances[cls]; ok {
				vcpu = info.VCPUs
				mem = info.MemoryGiB
				maxBandwidth = info.MaxBandwidthMbps
				break
			}
		}

		if vcpu == 0 && mem == 0 {
			// Skip classes with no valid hardware specs
			continue
		}

		// Build pricing map: presence of region key = available in that region
		pricing := make(map[string]float64)
		for _, region := range targetRegions {
			if regionInstances, ok := regionData[region]; ok {
				if info, ok := regionInstances[cls]; ok && info.Price > 0 {
					pricing[region] = info.Price
				}
			}
		}

		// Compute min engine version from DescribeOrderableDBInstanceOptions data
		minVersion := ""
		if classInfo != nil {
			if info, ok := classInfo[cls]; ok {
				minVersion = computeMinEngineVersion(info.engineVersions)
			}
		}

		props := types.InstanceProperties{
			Vcpu:             vcpu,
			Mem:              mem,
			MaxBandwidth:     maxBandwidth,
			Pricing:          pricing,
			MinEngineVersion: minVersion,
		}

		// Set StdPrice from the home region for backward compatibility
		if price, ok := pricing[homeRegion]; ok {
			props.StdPrice = price
		}

		// Compute max connections
		props.MaxConnections = GetMaxConnections(engine, cls, mem)

		instanceTypes[cls] = props
	}

	// Build up/down linked lists within each family
	status(fmt.Sprintf("[%s] Linking instance families...", engine))
	buildFamilyLinks(instanceTypes)

	status(fmt.Sprintf("[%s] Generated %d instance types", engine, len(instanceTypes)))
	return instanceTypes, nil
}

// listOrderableClassInfo calls DescribeOrderableDBInstanceOptions and returns
// per-class info including all supported engine versions.
func listOrderableClassInfo(ctx context.Context, cfg aws.Config, engine string) (map[string]*orderableClassInfo, error) {
	client := awsRds.NewFromConfig(cfg)

	result := make(map[string]*orderableClassInfo)

	paginator := awsRds.NewDescribeOrderableDBInstanceOptionsPaginator(client, &awsRds.DescribeOrderableDBInstanceOptionsInput{
		Engine: aws.String(engine),
	})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, opt := range output.OrderableDBInstanceOptions {
			if opt.DBInstanceClass == nil {
				continue
			}
			cls := *opt.DBInstanceClass

			if _, ok := result[cls]; !ok {
				result[cls] = &orderableClassInfo{}
			}

			// Collect engine version if available
			if opt.EngineVersion != nil {
				result[cls].engineVersions = append(result[cls].engineVersions, *opt.EngineVersion)
			}
		}
	}

	return result, nil
}

// regionResult holds the output of a per-region bulk JSON fetch.
type regionResult struct {
	region string
	data   map[string]BulkInstanceInfo
	err    error
}

// fetchMultiRegionData fetches bulk pricing JSON data across all target regions
// in parallel with a concurrency limit. Each region's bulk JSON provides hardware
// specs, pricing, and availability in a single download.
func fetchMultiRegionData(
	ctx context.Context,
	engine string,
	targetRegions []string,
	status func(string),
) (map[string]map[string]BulkInstanceInfo, error) {

	const concurrency = 10

	results := make(chan regionResult, len(targetRegions))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, region := range targetRegions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			status(fmt.Sprintf("[%s] Downloading bulk pricing for %s...", engine, region))

			data, err := FetchBulkInstanceData(ctx, engine, region)
			if err != nil {
				results <- regionResult{region: region, err: fmt.Errorf("bulk data for %s: %w", region, err)}
				return
			}

			status(fmt.Sprintf("[%s] Got %d instance types for %s", engine, len(data), region))
			results <- regionResult{
				region: region,
				data:   data,
			}
		}(region)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	regionData := make(map[string]map[string]BulkInstanceInfo)
	var firstErr error

	for res := range results {
		if res.err != nil {
			status(fmt.Sprintf("Warning: %v", res.err))
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		regionData[res.region] = res.data
	}

	return regionData, firstErr
}

// computeMinEngineVersion finds the minimum engine version from a list of version strings.
func computeMinEngineVersion(versions []string) string {
	if len(versions) == 0 {
		return ""
	}

	min := versions[0]
	for _, v := range versions[1:] {
		if util.CompareVersions(v, min) < 0 {
			min = v
		}
	}
	return min
}

// instanceFamilyKey extracts the family prefix from an instance class.
// e.g., "db.r6g.xlarge" -> "db.r6g", "db.t3.medium" -> "db.t3"
func instanceFamilyKey(cls string) string {
	parts := strings.Split(cls, ".")
	if len(parts) >= 3 {
		return parts[0] + "." + parts[1]
	}
	return cls
}

// instanceSizeOrder returns a numeric sort key for instance sizes.
var sizeOrder = map[string]int{
	"micro":    1,
	"small":    2,
	"medium":   3,
	"large":    4,
	"xlarge":   5,
	"2xlarge":  6,
	"4xlarge":  7,
	"8xlarge":  8,
	"12xlarge": 9,
	"16xlarge": 10,
	"24xlarge": 11,
	"32xlarge": 12,
	"48xlarge": 13,
}

func instanceSizeKey(cls string) int {
	parts := strings.Split(cls, ".")
	if len(parts) >= 3 {
		if order, ok := sizeOrder[parts[2]]; ok {
			return order
		}
	}
	return 99
}

// buildFamilyLinks groups instances by family and creates up/down pointers.
func buildFamilyLinks(instanceTypes types.InstanceTypes) {
	// Group by family
	families := make(map[string][]string)
	for cls := range instanceTypes {
		family := instanceFamilyKey(cls)
		families[family] = append(families[family], cls)
	}

	// Sort each family by size and link
	for _, members := range families {
		sort.Slice(members, func(i, j int) bool {
			return instanceSizeKey(members[i]) < instanceSizeKey(members[j])
		})

		for i, cls := range members {
			props := instanceTypes[cls]

			if i > 0 {
				down := members[i-1]
				props.Down = &down
			} else {
				props.Down = nil
			}

			if i < len(members)-1 {
				up := members[i+1]
				props.Up = &up
			} else {
				props.Up = nil
			}

			instanceTypes[cls] = props
		}
	}
}
