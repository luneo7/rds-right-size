package rds_right_size

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/config"
	cwTypes "github.com/luneo7/rds-right-size/internal/cw/types"
	"github.com/luneo7/rds-right-size/internal/rds-right-size/types"
	rdsTypes "github.com/luneo7/rds-right-size/internal/rds/types"
)

// MultiRegionOptions configures a parallel multi-region analysis.
type MultiRegionOptions struct {
	Regions          []string
	Profile          string
	InstanceTypesURL string
	Period           int
	Tags             rdsTypes.Tags
	CPUDownsize      float64
	CPUUpsize        float64
	MemUpsize        float64
	Stat             cwTypes.StatName
	PreferNewGen     bool
	FetchTimeSeries  bool

	// OnProgress is called with aggregated progress across all regions.
	// instanceLabel already includes the region suffix, e.g. "my-db (us-east-1)".
	OnProgress func(current, total int, instanceLabel string)

	// OnWarning is called when an instance is skipped. instanceLabel includes region.
	OnWarning func(instanceLabel, msg string)

	// OnRegionError is called when a region's analysis fails.
	// It is informational; analysis continues for other regions.
	// If all regions fail, AnalyzeMultiRegion returns an error.
	OnRegionError func(region string, err error)
}

// AnalyzeMultiRegion runs AnalyzeRDS in parallel across all regions in opts.Regions.
// Each region gets its own AWS config derived from opts.Profile.
// Results are merged, stamped with their region, sorted, and returned.
// Returns an error only if every region fails; partial success is surfaced via OnRegionError.
func AnalyzeMultiRegion(ctx context.Context, opts MultiRegionOptions) ([]types.Recommendation, []string, error) {
	type regionResult struct {
		region          string
		recommendations []types.Recommendation
		warnings        []string
		err             error
	}

	var mu sync.Mutex
	type progress struct{ current, total int }
	regionProg := make(map[string]*progress)
	for _, rgn := range opts.Regions {
		regionProg[rgn] = &progress{}
	}

	results := make([]regionResult, len(opts.Regions))
	var wg sync.WaitGroup

	for i, rgn := range opts.Regions {
		wg.Add(1)
		go func(idx int, rgn string) {
			defer wg.Done()

			var optFns []func(*config.LoadOptions) error
			if opts.Profile != "" {
				optFns = append(optFns, config.WithSharedConfigProfile(opts.Profile))
			}
			optFns = append(optFns, config.WithRegion(rgn))

			cfg, err := config.LoadDefaultConfig(ctx, optFns...)
			if err != nil {
				results[idx] = regionResult{region: rgn, err: err}
				return
			}

			instanceTypesURL := opts.InstanceTypesURL
			analyzer := NewRDSRightSize(
				&instanceTypesURL,
				&cfg,
				opts.Period,
				opts.Tags,
				opts.CPUDownsize,
				opts.CPUUpsize,
				opts.MemUpsize,
				opts.Stat,
				opts.PreferNewGen,
				rgn,
			)

			var regionWarnings []string
			analysisOpts := &AnalysisOptions{
				FetchTimeSeries: opts.FetchTimeSeries,
				OnProgress: func(current, total int, instanceId string) {
					if opts.OnProgress == nil {
						return
					}
					mu.Lock()
					rp := regionProg[rgn]
					rp.current = current
					rp.total = total
					var totalSum, currentSum int
					for _, p := range regionProg {
						totalSum += p.total
						currentSum += p.current
					}
					mu.Unlock()
					opts.OnProgress(currentSum, totalSum, fmt.Sprintf("%s (%s)", instanceId, rgn))
				},
				OnWarning: func(instanceId, msg string) {
					label := fmt.Sprintf("%s (%s)", instanceId, rgn)
					if opts.OnWarning != nil {
						opts.OnWarning(label, msg)
					}
					mu.Lock()
					regionWarnings = append(regionWarnings, fmt.Sprintf("%s: %s", label, msg))
					mu.Unlock()
				},
			}

			recommendations, err := analyzer.AnalyzeRDS(ctx, analysisOpts)
			if err != nil {
				results[idx] = regionResult{region: rgn, err: err}
				return
			}

			// Stamp region on each recommendation
			for j := range recommendations {
				recommendations[j].Region = rgn
			}
			results[idx] = regionResult{
				region:          rgn,
				recommendations: recommendations,
				warnings:        regionWarnings,
			}
		}(i, rgn)
	}

	wg.Wait()

	// Merge results
	allRecs := make([]types.Recommendation, 0)
	var allWarnings []string
	var errs []string

	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.region, r.err))
			if opts.OnRegionError != nil {
				opts.OnRegionError(r.region, r.err)
			}
			continue
		}
		allRecs = append(allRecs, r.recommendations...)
		allWarnings = append(allWarnings, r.warnings...)
	}

	if len(allRecs) == 0 && len(errs) > 0 {
		return nil, nil, fmt.Errorf("all regions failed:\n%s", strings.Join(errs, "\n"))
	}

	SortRecommendations(allRecs)

	return allRecs, allWarnings, nil
}
