package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/config"
	cwTypes "github.com/luneo7/rds-right-size/internal/cw/types"
	"github.com/luneo7/rds-right-size/internal/generator"
	rds "github.com/luneo7/rds-right-size/internal/rds-right-size"
	rdsTypes "github.com/luneo7/rds-right-size/internal/rds-right-size/types"
	"github.com/luneo7/rds-right-size/internal/tui"
)

const (
	defaultInstanceTypesURL = "https://gist.github.com/luneo7/1c331a4f7423cd2adeb2c70db55a9855/raw/33b87eb46f63b932f234b22bd7e1087ab07f1ffc/aurora_instance_types.json"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "generate-types" {
		runGenerateTypes()
		return
	}

	runAnalyze()
}

// runAnalyze handles the default analyze behavior (original CLI + TUI mode).
func runAnalyze() {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)

	var (
		profile          string
		region           string
		tags             string
		instanceTypesUrl string
		statName         string
		period           int
		cpuUpsize        float64
		cpuDownsize      float64
		memUpsize        float64
		preferNewGen     bool
		tuiMode          bool
	)

	fs.StringVar(&profile, "profile", "", "The name of the profile to log in with")
	fs.StringVar(&profile, "p", "", "The name of the profile to log in with (shorthand)")
	fs.StringVar(&tags, "tags", "", "Comma separated key/value tags map to filter instances")
	fs.StringVar(&tags, "t", "", "Comma separated key/value tags map to filter instances (shorthand)")
	fs.IntVar(&period, "period", 30, "Lookback period in days")
	fs.IntVar(&period, "pe", 30, "Lookback period in days (shorthand)")
	fs.Float64Var(&cpuUpsize, "cpu-upsize", 75, "Average used CPU % - Upsize threshold")
	fs.Float64Var(&cpuUpsize, "cu", 75, "Average used CPU % - Upsize threshold (shorthand)")
	fs.Float64Var(&cpuDownsize, "cpu-downsize", 30, "Average used CPU % - Downsize Threshold")
	fs.Float64Var(&cpuDownsize, "cd", 30, "Average used CPU % - Downsize Threshold (shorthand)")
	fs.Float64Var(&memUpsize, "mem-upsize", 5, "Freeable Memory % of Instance Memory - Upsize threshold")
	fs.Float64Var(&memUpsize, "mu", 5, "Freeable Memory % of Instance Memory - Upsize threshold (shorthand)")
	fs.StringVar(&region, "region", "", "AWS Region(s) to analyze (comma-separated for multi-region)")
	fs.StringVar(&region, "r", "", "AWS Region(s) to analyze (shorthand)")
	fs.StringVar(&instanceTypesUrl, "instance-types", defaultInstanceTypesURL, "Instance types JSON URL or local file path")
	fs.StringVar(&instanceTypesUrl, "i", defaultInstanceTypesURL, "Instance types JSON URL or local file path (shorthand)")
	fs.StringVar(&statName, "stat", "p99", "Statistic to be used to determine down/upsizing (ex.: Average, p99, p95, p50)")
	fs.StringVar(&statName, "s", "p99", "Statistic to be used to determine down/upsizing (shorthand)")
	fs.BoolVar(&preferNewGen, "prefer-new-gen", false, "Prefer newer instance generation when scaling (e.g., r6g -> r7g)")
	fs.BoolVar(&preferNewGen, "ng", false, "Prefer newer instance generation when scaling (shorthand)")
	fs.BoolVar(&tuiMode, "tui", false, "Launch interactive TUI mode")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: Unused command line arguments detected.\n")
		fs.Usage()
		os.Exit(2)
	}

	if tuiMode {
		defaults := tui.ConfigValues{
			Profile:          profile,
			Region:           region,
			Tags:             tags,
			Period:           period,
			CPUUpsize:        cpuUpsize,
			CPUDownsize:      cpuDownsize,
			MemUpsize:        memUpsize,
			Stat:             statName,
			PreferNewGen:     preferNewGen,
			InstanceTypesURL: instanceTypesUrl,
		}

		if err := tui.Run(defaults); err != nil {
			fmt.Printf("TUI error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Original CLI behavior
	regions := splitRegions(region)

	if len(regions) <= 1 {
		// Single region — existing behavior
		var optFns []func(*config.LoadOptions) error

		if profile != "" {
			optFns = append(optFns, config.WithSharedConfigProfile(profile))
		}

		if region != "" {
			optFns = append(optFns, config.WithRegion(region))
		}

		cfg, err := config.LoadDefaultConfig(context.Background(), optFns...)

		if err != nil {
			fmt.Printf("Fail to get AWS config: %v", err)
			os.Exit(1)
		}

		err = rds.NewRDSRightSize(&instanceTypesUrl, &cfg, period, parseTags(tags), cpuDownsize, cpuUpsize, memUpsize, cwTypes.StatName(statName), preferNewGen, region).DoAnalyzeRDS()

		if err != nil {
			log.Fatal(err)
		}
		return
	}

	// Multi-region parallel analysis
	type regionResult struct {
		region          string
		recommendations []rdsTypes.Recommendation
		err             error
	}

	results := make([]regionResult, len(regions))
	var wg sync.WaitGroup

	for i, rgn := range regions {
		wg.Add(1)
		go func(idx int, rgn string) {
			defer wg.Done()

			var optFns []func(*config.LoadOptions) error
			if profile != "" {
				optFns = append(optFns, config.WithSharedConfigProfile(profile))
			}
			optFns = append(optFns, config.WithRegion(rgn))

			cfg, err := config.LoadDefaultConfig(context.Background(), optFns...)
			if err != nil {
				results[idx] = regionResult{region: rgn, err: err}
				return
			}

			instanceTypesURL := instanceTypesUrl
			analyzer := rds.NewRDSRightSize(&instanceTypesURL, &cfg, period, parseTags(tags), cpuDownsize, cpuUpsize, memUpsize, cwTypes.StatName(statName), preferNewGen, rgn)
			recommendations, err := analyzer.AnalyzeRDS(&rds.AnalysisOptions{
				OnWarning: func(instanceId, msg string) {
					fmt.Fprintf(os.Stderr, "Warning: skipping instance %s (%s): %s\n", instanceId, rgn, msg)
				},
			})
			if err != nil {
				results[idx] = regionResult{region: rgn, err: err}
				return
			}

			for j := range recommendations {
				recommendations[j].Region = rgn
			}
			results[idx] = regionResult{region: rgn, recommendations: recommendations}
		}(i, rgn)
	}

	wg.Wait()

	allRecs := make([]rdsTypes.Recommendation, 0)
	var errs []string
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.region, r.err))
			fmt.Fprintf(os.Stderr, "Warning: %s analysis failed: %v\n", r.region, r.err)
			continue
		}
		allRecs = append(allRecs, r.recommendations...)
	}

	if len(allRecs) == 0 && len(errs) > 0 {
		log.Fatalf("All regions failed:\n%s", strings.Join(errs, "\n"))
	}

	// Sort: cluster members grouped, then by instance ID
	sort.SliceStable(allRecs, func(i, j int) bool {
		ci := allRecs[i].DBClusterIdentifier
		cj := allRecs[j].DBClusterIdentifier
		if ci != nil && cj == nil {
			return true
		}
		if ci == nil && cj != nil {
			return false
		}
		if ci != nil && cj != nil && *ci != *cj {
			return *ci < *cj
		}
		ii := allRecs[i].DBInstanceIdentifier
		ij := allRecs[j].DBInstanceIdentifier
		if ii != nil && ij != nil {
			return *ii < *ij
		}
		return false
	})

	if err := rds.WriteResultsCLI(allRecs); err != nil {
		log.Fatal(err)
	}
}

// runGenerateTypes handles the generate-types subcommand.
func runGenerateTypes() {
	fs := flag.NewFlagSet("generate-types", flag.ExitOnError)

	var (
		engine        string
		region        string
		profile       string
		output        string
		targetRegions string
	)

	fs.StringVar(&engine, "engine", "both", "Database engine (both, aurora-mysql, or aurora-postgresql)")
	fs.StringVar(&engine, "e", "both", "Database engine (shorthand)")
	fs.StringVar(&region, "region", "", "AWS region for orderable instances and pricing")
	fs.StringVar(&region, "r", "", "AWS region (shorthand)")
	fs.StringVar(&profile, "profile", "", "AWS profile to use")
	fs.StringVar(&profile, "p", "", "AWS profile (shorthand)")
	fs.StringVar(&output, "output", "aurora_instance_types.json", "Output file path")
	fs.StringVar(&output, "o", "aurora_instance_types.json", "Output file path (shorthand)")
	fs.StringVar(&targetRegions, "target-regions", "all", "Target regions for pricing/availability (comma-separated or 'all')")
	fs.StringVar(&targetRegions, "tr", "all", "Target regions (shorthand)")

	// Parse from os.Args[2:] since os.Args[1] is "generate-types"
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	if region == "" {
		fmt.Fprintf(os.Stderr, "Error: --region is required for generate-types\n")
		fs.Usage()
		os.Exit(2)
	}

	if engine != "both" && engine != "aurora-mysql" && engine != "aurora-postgresql" {
		fmt.Fprintf(os.Stderr, "Error: --engine must be 'both', 'aurora-mysql', or 'aurora-postgresql'\n")
		fs.Usage()
		os.Exit(2)
	}

	// Build AWS config
	var optFns []func(*config.LoadOptions) error
	if profile != "" {
		optFns = append(optFns, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		optFns = append(optFns, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), optFns...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load AWS config: %v\n", err)
		os.Exit(1)
	}

	opts := generator.GenerateOptions{
		Engine:        engine,
		Region:        region,
		TargetRegions: targetRegions,
		Output:        output,
		OnStatus: func(status string) {
			fmt.Println(status)
		},
	}

	if err := generator.GenerateInstanceTypes(context.Background(), cfg, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseTags(tags string) map[string]string {
	tagsEntries := strings.Split(tags, ",")

	tagsMap := make(map[string]string)

	if len(tagsEntries) > 0 {
		for _, e := range tagsEntries {
			if len(strings.TrimSpace(e)) > 0 {
				parts := strings.Split(e, "=")
				if len(parts) == 2 {
					tagsMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	return tagsMap
}

// splitRegions splits a comma-separated region string into a slice,
// trimming whitespace and filtering empty entries.
func splitRegions(s string) []string {
	var regions []string
	for _, r := range strings.Split(s, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			regions = append(regions, r)
		}
	}
	return regions
}
