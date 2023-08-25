package rds_right_size

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go/ptr"
	"github.com/luneo7/go-rds-right-size/internal/cw"
	cwTypes "github.com/luneo7/go-rds-right-size/internal/cw/types"
	"github.com/luneo7/go-rds-right-size/internal/rds"
	"github.com/luneo7/go-rds-right-size/internal/rds-right-size/types"
	rdsTypes "github.com/luneo7/go-rds-right-size/internal/rds/types"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	mbit_bytes  = 131072
	hours_month = 730
)

type RDSRightSize struct {
	rds                  *rds.RDS
	cloudWatch           *cw.CloudWatch
	period               int
	tags                 rdsTypes.Tags
	instanceTypes        types.InstanceTypes
	armInstanceRegex     *regexp.Regexp
	cpuDownsizeThreshold float64
	cpuUpsizeThreshold   float64
	memUpsizeThreshold   float64
}

func NewRDSRightSize(instanceTypesUrl *string, awsConfig *aws.Config, period int, tags rdsTypes.Tags, cpuDownsizeThreshold float64, cpuUpsizeThreshold float64, memUpsizeThreshold float64) *RDSRightSize {
	return &RDSRightSize{
		rds:                  rds.NewRDS(awsConfig),
		cloudWatch:           cw.NewCloudWatch(awsConfig),
		period:               period,
		tags:                 tags,
		instanceTypes:        loadInstanceTypes(instanceTypesUrl),
		armInstanceRegex:     regexp.MustCompile(`db\..*g\..*`),
		cpuDownsizeThreshold: cpuDownsizeThreshold,
		cpuUpsizeThreshold:   cpuUpsizeThreshold,
		memUpsizeThreshold:   memUpsizeThreshold,
	}
}

func (r *RDSRightSize) DoAnalyzeRDS() error {
	recommendations := make([]types.Recommendation, 0)
	instances, err := r.rds.GetInstances()
	if err != nil {
		return err
	}

	for _, instance := range instances {
		requiredTags := r.hasRequiredTags(&instance)

		if *requiredTags {
			metrics, err := r.getMetrics(&instance)

			if err != nil {
				return err
			}

			noConnections, err := r.hadNoConnections(metrics)
			if err != nil {
				return err
			}

			if *noConnections {
				recommendations = append(recommendations, types.Recommendation{
					Instance:       instance,
					Recommendation: types.Terminate,
					Reason:         types.NoUsageWithinPeriodReason,
				})
			} else {
				instanceProperties, mappedInstance := r.instanceTypes[*instance.DBInstanceClass]

				if mappedInstance {
					memoryUtilization, err := r.getMemoryUtilization(metrics, &instanceProperties)
					if err != nil {
						return err
					}

					if *memoryUtilization.UnderProvisioned && instanceProperties.Up != nil {
						upInstance := r.instanceTypes[*instanceProperties.Up]
						recommendations = append(recommendations, types.Recommendation{
							Instance:                    instance,
							Recommendation:              types.UpScale,
							Reason:                      types.MemoryUnderProvisionedReason,
							RecommendedInstanceType:     instanceProperties.Up,
							MetricValue:                 memoryUtilization.Value,
							MonthlyApproximatePriceDiff: Float64((upInstance.StdPrice - instanceProperties.StdPrice) * hours_month),
						})
					} else {
						cpuUtilization, err := r.getCPUUtilization(metrics)
						if err != nil {
							return err
						}

						bandwidthUtilization, err := r.getBandwidthUtilization(metrics, &instanceProperties)

						if err != nil {
							return err
						}

						if cpuUtilization.Status == types.CPUUnderProvisioned && instanceProperties.Up != nil {
							upInstance := r.instanceTypes[*instanceProperties.Up]
							recommendations = append(recommendations, types.Recommendation{
								Instance:                    instance,
								Recommendation:              types.UpScale,
								Reason:                      types.CPUUnderProvisionedReason,
								RecommendedInstanceType:     instanceProperties.Up,
								MetricValue:                 cpuUtilization.Value,
								MonthlyApproximatePriceDiff: Float64((upInstance.StdPrice - instanceProperties.StdPrice) * hours_month),
							})
						} else if cpuUtilization.Status == types.CPUOverProvisioned && bandwidthUtilization.Status != types.BandwidthUnderProvisioned && instanceProperties.Down != nil {
							downInstance := r.instanceTypes[*instanceProperties.Down]
							if downInstance.MaxBandwidth != nil && *bandwidthUtilization.Total < float64(*downInstance.MaxBandwidth*mbit_bytes) {
								recommendations = append(recommendations, types.Recommendation{
									Instance:                    instance,
									Recommendation:              types.DownScale,
									Reason:                      types.CPUOverProvisionedReason,
									RecommendedInstanceType:     instanceProperties.Down,
									MetricValue:                 cpuUtilization.Value,
									MonthlyApproximatePriceDiff: Float64((downInstance.StdPrice - instanceProperties.StdPrice) * hours_month),
								})
							}
						}
					}
				}
			}
		}
	}

	r.writeApproximateCostDifference(recommendations)

	return r.writeRecommendations(recommendations)
}

func Float64(v float64) *float64 {
	return ptr.Float64(v)
}

func (r *RDSRightSize) writeApproximateCostDifference(recommendations []types.Recommendation) {
	var priceDiff float64 = 0
	for _, recommendation := range recommendations {
		if recommendation.MonthlyApproximatePriceDiff != nil {
			priceDiff = priceDiff + *recommendation.MonthlyApproximatePriceDiff
		}
	}

	if priceDiff != 0 {
		if priceDiff > 0 {
			fmt.Println(fmt.Sprintf("The changes will yield a price increase of approximately $%.2f per month", priceDiff))
		} else {
			fmt.Println(fmt.Sprintf("The changes will yield a savings of approximately $%.2f per month", priceDiff*-1))
		}
	}

}

func (r *RDSRightSize) findNewInstanceClassWhenDownsizing(currentInstanceProperties *types.InstanceProperties, currentInstance *rdsTypes.Instance) *string {
	if strings.HasPrefix(*currentInstance.DBInstanceClass, "db.t") {
		return nil
	}

	var down *string
	var chosen types.InstanceProperties
	prefix := "db.t3"

	currantInstanceIsArm := r.armInstanceRegex.MatchString(*currentInstance.DBInstanceClass)

	if currantInstanceIsArm {
		prefix = "db.t4g"
	}

	for instanceClass, properties := range r.instanceTypes {
		if strings.HasPrefix(instanceClass, prefix) {
			if properties.Vcpu <= currentInstanceProperties.Vcpu {
				if down == nil || (properties.Vcpu >= chosen.Vcpu && properties.Mem >= chosen.Mem) {
					newInstanceClass := strings.Clone(instanceClass)
					down = &newInstanceClass
					chosen = properties
				}
			}
		}
	}

	return down
}

func (r *RDSRightSize) writeRecommendations(recommendations []types.Recommendation) error {
	data, err := json.MarshalIndent(recommendations, "", "  ")
	if err != nil {
		return err
	}

	filename := r.getFilenameWithDate()
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return err
	}

	absPath, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	fmt.Println(absPath)
	return nil
}

func (r *RDSRightSize) getFilenameWithDate() string {
	const layout = "2006-01-02 15:04:05"
	t := time.Now()
	return "recommendations-" + t.Format(layout) + ".json"
}

func (r *RDSRightSize) hasRequiredTags(instance *rdsTypes.Instance) *bool {
	returnValue := true

	if len(r.tags) > 0 {
		tagsFound := 0

		for key, value := range r.tags {
			val, hasValue := instance.Tags[key]
			if hasValue && value == val {
				tagsFound++
			}
		}

		returnValue = tagsFound == len(r.tags)
	}

	return &returnValue
}

func (r *RDSRightSize) hadNoConnections(metrics *cwTypes.Metrics) (*bool, error) {
	var returnValue bool

	metric, ok := metrics.InstanceMetrics[cwTypes.DatabaseConnections]

	if !ok {
		return nil, errors.New("no database connections metric found")
	}

	if metric.Value == nil || *metric.Value == 0 {
		returnValue = true
	} else {
		returnValue = false
	}

	return &returnValue, nil
}

func (r *RDSRightSize) getMemoryUtilization(metrics *cwTypes.Metrics, instanceProperties *types.InstanceProperties) (*types.MemoryUtilization, error) {
	var returnValue types.MemoryUtilization
	var underProvisioned bool

	metric, ok := metrics.InstanceMetrics[cwTypes.FreeableMemory]

	if !ok {
		return nil, errors.New("no freeable memory metric found")
	}

	metricValue := (*metric.Value / (1 << 30)) * 100.0 / float64((*instanceProperties).Mem)

	if metricValue < r.memUpsizeThreshold {
		underProvisioned = true
	} else {
		underProvisioned = false
	}

	returnValue = types.MemoryUtilization{
		Value:            &metricValue,
		UnderProvisioned: &underProvisioned,
	}

	return &returnValue, nil
}

func (r *RDSRightSize) getMetrics(instance *rdsTypes.Instance) (*cwTypes.Metrics, error) {
	return r.cloudWatch.GetMetrics(instance.DBInstanceIdentifier, r.period)
}

func (r *RDSRightSize) getBandwidthUtilization(metrics *cwTypes.Metrics, instanceProperties *types.InstanceProperties) (*types.BandwidthUtilization, error) {
	var returnValue types.BandwidthUtilization

	readMetric, ok := metrics.InstanceMetrics[cwTypes.ReadThroughput]

	if !ok {
		return nil, errors.New("no read throughput metric found")
	}

	writeMetric, ok := metrics.InstanceMetrics[cwTypes.WriteThroughput]

	if !ok {
		return nil, errors.New("no write throughput metric found")
	}

	total := *readMetric.Value + *writeMetric.Value

	if instanceProperties.MaxBandwidth != nil {
		metricValue := total / float64(*instanceProperties.MaxBandwidth*mbit_bytes) * 100.0

		if metricValue > r.cpuUpsizeThreshold {
			returnValue = types.BandwidthUtilization{
				Value:  &metricValue,
				Total:  &total,
				Status: types.BandwidthUnderProvisioned,
			}
		} else if metricValue >= r.cpuDownsizeThreshold {
			returnValue = types.BandwidthUtilization{
				Value:  &metricValue,
				Total:  &total,
				Status: types.BandwidthOptimized,
			}
		} else {
			returnValue = types.BandwidthUtilization{
				Value:  &metricValue,
				Total:  &total,
				Status: types.BandwidthOverProvisioned,
			}
		}
	} else {
		returnValue = types.BandwidthUtilization{
			Total:  &total,
			Status: types.BandwidthOptimized,
		}
	}

	return &returnValue, nil
}

func (r *RDSRightSize) getCPUUtilization(metrics *cwTypes.Metrics) (*types.CPUUtilization, error) {
	var returnValue types.CPUUtilization

	metric, ok := metrics.InstanceMetrics[cwTypes.CPUUtilization]

	if !ok {
		return nil, errors.New("no cpu utilization metric found")
	}

	if *metric.Value > r.cpuUpsizeThreshold {
		returnValue = types.CPUUtilization{
			Value:  metric.Value,
			Status: types.CPUUnderProvisioned,
		}
	} else if *metric.Value >= r.cpuDownsizeThreshold {
		returnValue = types.CPUUtilization{
			Value:  metric.Value,
			Status: types.CPUOptimized,
		}
	} else {
		returnValue = types.CPUUtilization{
			Value:  metric.Value,
			Status: types.CPUOverProvisioned,
		}
	}

	return &returnValue, nil
}

func loadInstanceTypes(instanceTypesUrl *string) types.InstanceTypes {
	httpClient := http.Client{
		Timeout: time.Second * 2,
	}

	req, err := http.NewRequest(http.MethodGet, *instanceTypesUrl, nil)

	if err != nil {
		log.Fatal(err)
	}

	res, getErr := httpClient.Do(req)

	if getErr != nil {
		log.Fatal(getErr)
	}

	if res.Body != nil {
		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(res.Body)
	}

	body, readErr := io.ReadAll(res.Body)

	if readErr != nil {
		log.Fatal(readErr)
	}

	instanceTypes := types.InstanceTypes{}
	jsonErr := json.Unmarshal(body, &instanceTypes)

	if jsonErr != nil {
		log.Fatal(jsonErr)
	}

	return instanceTypes
}
