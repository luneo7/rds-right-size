package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"log"
	"os"
	"strings"

	rds "github.com/luneo7/go-rds-right-size/internal/rds-right-size"
)

var (
	profile          string
	region           string
	tags             string
	instanceTypesUrl string
	period           int
	cpuUpsize        float64
	cpuDownsize      float64
	memUpsize        float64
)

func init() {
	const (
		profileDefaultValue       = ""
		profileUsage              = "The name of the profile to log in with"
		tagsDefaultValue          = ""
		tagsUsage                 = "Comma separated key/value tags map to filter instances"
		periodDefaultValue        = 30
		periodUsage               = "Lookback period in days, default of 30 days"
		regionDefaultValue        = ""
		regionUsage               = "AWS Region to analyze"
		cpuUpsizeDefaultValue     = 75
		cpuUpsizeUsage            = "Average used CPU % - Upsize threshold"
		cpuDownsizeDefaultVale    = 45
		cpuDownsizeUsage          = "Average used CPU % - Downsize Threshold"
		memUpsizeDefaultVale      = 10
		memUpsizeUsage            = "Freeable Memory % of Instance Memory - Upsize threshold"
		instanceTypesDefaultValue = "https://gist.githubusercontent.com/luneo7/ff148e252dccb8bf14f3c9703c695cff/raw/231b9fd9ea34f8dd15aeb61ff8943690d06acccb/rds_instance_types.json"
		instanceTypeUsage         = "Instance types JSON URL"
	)

	flag.StringVar(&profile, "profile", profileDefaultValue, profileUsage)
	flag.StringVar(&profile, "p", profileDefaultValue, profileUsage+" (shorthand)")
	flag.StringVar(&tags, "tags", tagsDefaultValue, tagsUsage)
	flag.StringVar(&tags, "t", tagsDefaultValue, tagsUsage+" (shorthand)")
	flag.IntVar(&period, "period", periodDefaultValue, periodUsage)
	flag.IntVar(&period, "pe", periodDefaultValue, periodUsage+" (shorthand)")
	flag.Float64Var(&cpuUpsize, "cpu-upsize", cpuUpsizeDefaultValue, cpuUpsizeUsage)
	flag.Float64Var(&cpuUpsize, "cu", cpuUpsizeDefaultValue, cpuUpsizeUsage+" (shorthand)")
	flag.Float64Var(&cpuDownsize, "cpu-downsize", cpuDownsizeDefaultVale, cpuDownsizeUsage)
	flag.Float64Var(&cpuDownsize, "cd", cpuDownsizeDefaultVale, cpuDownsizeUsage+" (shorthand)")
	flag.Float64Var(&memUpsize, "mem-upsize", memUpsizeDefaultVale, memUpsizeUsage)
	flag.Float64Var(&memUpsize, "mu", memUpsizeDefaultVale, memUpsizeUsage+" (shorthand)")
	flag.StringVar(&region, "region", regionDefaultValue, regionUsage)
	flag.StringVar(&region, "r", regionDefaultValue, regionUsage+" (shorthand)")
	flag.StringVar(&instanceTypesUrl, "instance-types", instanceTypesDefaultValue, instanceTypeUsage)
	flag.StringVar(&instanceTypesUrl, "i", instanceTypesDefaultValue, instanceTypeUsage+" (shorthand)")

	flag.Parse()
	if flag.NArg() > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Error: Unused command line arguments detected.\n")
		flag.Usage()
		os.Exit(2)
	}
}

func main() {
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

	tagsEntries := strings.Split(tags, ",")

	tagsMap := make(map[string]string)

	if len(tagsEntries) > 1 {
		for _, e := range tagsEntries {
			if len(strings.TrimSpace(e)) > 0 {
				parts := strings.Split(e, "=")
				if len(parts) == 2 {
					tagsMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	err = rds.NewRDSRightSize(&instanceTypesUrl, &cfg, period, tagsMap, cpuDownsize, cpuUpsize, memUpsize).DoAnalyzeRDS()

	if err != nil {
		log.Fatal(err)
	}
}
