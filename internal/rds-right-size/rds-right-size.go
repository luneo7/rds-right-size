package rds_right_size

import (
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
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
	var recommendations []types.Recommendation
	instances, err := r.rds.GetInstances()
	if err != nil {
		return err
	}

	for _, instance := range instances {
		requiredTags := r.hasRequiredTags(&instance)

		if *requiredTags {
			noConnections, err := r.hadNoConnections(&instance)
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
					memoryUtilization, err := r.getMemoryUtilization(&instance, &instanceProperties)
					if err != nil {
						return err
					}

					if *memoryUtilization.UnderProvisioned {
						recommendations = append(recommendations, types.Recommendation{
							Instance:                instance,
							Recommendation:          types.UpScale,
							Reason:                  types.MemoryUnderProvisionedReason,
							RecommendedInstanceType: instanceProperties.Up,
							MetricValue:             memoryUtilization.Value,
						})
					} else {
						cpuUtilization, err := r.getCPUUtilization(&instance)
						if err != nil {
							return err
						}

						if cpuUtilization.Status == types.CPUUnderProvisioned {
							recommendations = append(recommendations, types.Recommendation{
								Instance:                instance,
								Recommendation:          types.UpScale,
								Reason:                  types.CPUUnderProvisionedReason,
								RecommendedInstanceType: instanceProperties.Up,
								MetricValue:             cpuUtilization.Value,
							})
						} else if cpuUtilization.Status == types.CPUOverProvisioned {
							down := instanceProperties.Down
							if down == nil {
								down = r.findNewInstanceClassWhenDownsizing(&instanceProperties, &instance)
							}
							if down != nil {
								recommendations = append(recommendations, types.Recommendation{
									Instance:                instance,
									Recommendation:          types.DownScale,
									Reason:                  types.CPUOverProvisionedReason,
									RecommendedInstanceType: down,
									MetricValue:             cpuUtilization.Value,
								})
							}
						}
					}
				}
			}
		}
	}

	return r.writeRecommendations(recommendations)
}

func (r *RDSRightSize) findNewInstanceClassWhenDownsizing(currentInstanceProperties *types.InstanceProperties, currentInstance *rdsTypes.Instance) *string {
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

func (r *RDSRightSize) hadNoConnections(instance *rdsTypes.Instance) (*bool, error) {
	var returnValue bool

	dbConnections, err := r.cloudWatch.GetMetric(
		instance.DBInstanceIdentifier,
		r.period,
		cwTypes.DatabaseConnections,
	)

	if err != nil {
		return nil, err
	}

	if dbConnections.Average == nil || *dbConnections.Average == 0 {
		returnValue = true
	} else {
		returnValue = false
	}

	return &returnValue, nil
}

func (r *RDSRightSize) getMemoryUtilization(instance *rdsTypes.Instance, instanceProperties *types.InstanceProperties) (*types.MemoryUtilization, error) {
	var returnValue types.MemoryUtilization
	var underProvisioned bool

	freeableMemory, err := r.cloudWatch.GetMetric(
		instance.DBInstanceIdentifier,
		r.period,
		cwTypes.FreeableMemory,
	)

	if err != nil {
		return nil, err
	}

	metricValue := *freeableMemory.Average / float64((*instanceProperties).Mem)

	if metricValue < r.memUpsizeThreshold {
		underProvisioned = true
	} else {
		underProvisioned = false
	}

	returnValue = types.MemoryUtilization{
		Value:            &metricValue,
		UnderProvisioned: &underProvisioned,
	}

	return &returnValue, err
}

func (r *RDSRightSize) getCPUUtilization(instance *rdsTypes.Instance) (*types.CPUUtilization, error) {
	var returnValue types.CPUUtilization

	cpuUtilization, err := r.cloudWatch.GetMetric(
		instance.DBInstanceIdentifier,
		r.period,
		cwTypes.CPUUtilization,
	)

	if err != nil {
		return nil, err
	}

	if *cpuUtilization.Average > r.cpuUpsizeThreshold {
		returnValue = types.CPUUtilization{
			Value:  cpuUtilization.Average,
			Status: types.CPUUnderProvisioned,
		}
	} else if *cpuUtilization.Average >= r.cpuDownsizeThreshold {
		returnValue = types.CPUUtilization{
			Value:  cpuUtilization.Average,
			Status: types.CPUOptimized,
		}
	} else {
		returnValue = types.CPUUtilization{
			Value:  cpuUtilization.Average,
			Status: types.CPUOverProvisioned,
		}
	}

	return &returnValue, err
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
