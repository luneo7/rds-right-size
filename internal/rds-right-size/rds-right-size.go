package rds_right_size

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go/ptr"
	"github.com/luneo7/rds-right-size/internal/cw"
	cwTypes "github.com/luneo7/rds-right-size/internal/cw/types"
	"github.com/luneo7/rds-right-size/internal/rds"
	"github.com/luneo7/rds-right-size/internal/rds-right-size/types"
	rdsTypes "github.com/luneo7/rds-right-size/internal/rds/types"
	"github.com/luneo7/rds-right-size/internal/util"
)

const (
	mbit_bytes  = 131072
	hours_month = 730
)

// ProgressCallback is called during analysis to report progress.
// current is the instance index being analyzed (1-based), total is the total count,
// and instanceId is the identifier of the instance being processed.
type ProgressCallback func(current int, total int, instanceId string)

// AnalysisOptions controls optional behaviors of the analysis.
type AnalysisOptions struct {
	// FetchTimeSeries controls whether daily time-series metrics are fetched
	// for each instance (needed for TUI graphs). Defaults to false.
	FetchTimeSeries bool

	// OnProgress is an optional callback invoked for each instance analyzed.
	OnProgress ProgressCallback

	// OnWarning is an optional callback invoked when an instance is skipped
	// due to missing CloudWatch metrics (e.g., transient auto-scaling replicas).
	OnWarning func(instanceId string, msg string)
}

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
	statistic            cwTypes.StatName
	preferNewGen         bool
	region               string
	// maxConnCache caches GetMaxConnections results per parameter group name
	maxConnCache map[string]*int64
}

// clusterInstanceInfo tracks per-instance analysis data for cluster equalization.
type clusterInstanceInfo struct {
	instance   rdsTypes.Instance
	properties *types.InstanceProperties
	cpuValue   *float64
	peakConns  *float64
	tsMetrics  *cwTypes.TimeSeriesMetrics
	recIndex   int // index in recommendations slice, or -1 if no recommendation
}

func NewRDSRightSize(instanceTypesUrl *string, awsConfig *aws.Config, period int, tags rdsTypes.Tags, cpuDownsizeThreshold float64, cpuUpsizeThreshold float64, memUpsizeThreshold float64, statistic cwTypes.StatName, preferNewGen bool, region string) *RDSRightSize {
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
		statistic:            statistic,
		preferNewGen:         preferNewGen,
		region:               region,
		maxConnCache:         make(map[string]*int64),
	}
}

// GetInstanceTypes returns the loaded instance types map (used by TUI for display).
func (r *RDSRightSize) GetInstanceTypes() types.InstanceTypes {
	return r.instanceTypes
}

// lookupInstanceProperties resolves an instance class name to its properties,
// supporting both engine-prefixed keys (from "both" engine generation) and
// plain keys (from single-engine generation).
//
// It first tries the key as-is (handles both prefixed Up/Down chain names and
// plain single-engine keys). If that fails and engine is provided, it tries
// the engine-prefixed key "{engine}:{key}".
func (r *RDSRightSize) lookupInstanceProperties(key string, engine *string) (types.InstanceProperties, bool) {
	// Try key as-is first
	if props, ok := r.instanceTypes[key]; ok {
		return props, true
	}
	// Try engine-prefixed key (when key is plain but JSON uses prefixed keys)
	if engine != nil && *engine != "" {
		if props, ok := r.instanceTypes[*engine+":"+key]; ok {
			return props, true
		}
	}
	return types.InstanceProperties{}, false
}

// stripEnginePrefix removes the engine prefix from a key if present.
// e.g., "aurora-mysql:db.r6g.large" -> "db.r6g.large", "db.r6g.large" -> "db.r6g.large"
func stripEnginePrefix(key string) string {
	if idx := strings.Index(key, ":"); idx >= 0 {
		return key[idx+1:]
	}
	return key
}

// sameInstanceClass checks if two instance type keys refer to the same DB instance class,
// accounting for engine-prefixed keys (e.g., "aurora-mysql:db.r6g.large" matches "db.r6g.large").
func sameInstanceClass(a, b string) bool {
	return stripEnginePrefix(a) == stripEnginePrefix(b)
}

// instanceFamilyInfo holds the parsed components of a DB instance class name.
// e.g., "db.r6g.xlarge" → {prefix:"r", gen:6, suffix:"g", size:"xlarge"}
type instanceFamilyInfo struct {
	prefix string // letter prefix: "r", "m", "t", "x"
	gen    int    // generation number: 5, 6, 7
	suffix string // architecture/variant suffix: "g", "gd", "i", ""
	size   string // instance size: "large", "xlarge", "2xlarge"
}

// instanceFamilyRegex parses DB instance class names: db.{letters}{digits}{letters}.{size}
var instanceFamilyRegex = regexp.MustCompile(`^db\.([a-z]+)(\d+)([a-z]*)\.(.+)$`)

// parseInstanceFamily extracts family components from a DB instance class name.
// Accepts both plain ("db.r6g.xlarge") and engine-prefixed ("aurora-mysql:db.r6g.xlarge") keys.
func parseInstanceFamily(dbInstanceClass string) (instanceFamilyInfo, bool) {
	bare := stripEnginePrefix(dbInstanceClass)
	matches := instanceFamilyRegex.FindStringSubmatch(bare)
	if matches == nil {
		return instanceFamilyInfo{}, false
	}
	gen := 0
	for _, c := range matches[2] {
		gen = gen*10 + int(c-'0')
	}
	return instanceFamilyInfo{
		prefix: matches[1],
		gen:    gen,
		suffix: matches[3],
		size:   matches[4],
	}, true
}



// upgradeGeneration attempts to find a newer-generation instance with the same
// family prefix, architecture suffix, and size in the loaded instance types.
// Strict suffix matching ensures no architecture change (e.g., r6g → r7g only, never r6g → r7).
// It also verifies regional availability and engine version compatibility.
// Returns the map key, properties, and true if a newer generation was found.
func (r *RDSRightSize) upgradeGeneration(targetClass string, engine *string, engineVersion string) (string, types.InstanceProperties, bool) {
	targetInfo, ok := parseInstanceFamily(targetClass)
	if !ok {
		return "", types.InstanceProperties{}, false
	}

	var bestKey string
	var bestProps types.InstanceProperties
	bestGen := targetInfo.gen

	for key := range r.instanceTypes {
		bare := stripEnginePrefix(key)
		info, ok := parseInstanceFamily(bare)
		if !ok {
			continue
		}

		// Must match prefix, suffix, and size exactly
		if info.prefix != targetInfo.prefix || info.suffix != targetInfo.suffix || info.size != targetInfo.size {
			continue
		}

		// Must be a newer generation
		if info.gen <= bestGen {
			continue
		}

		// If using engine-prefixed keys, ensure engine matches
		if engine != nil && *engine != "" && strings.Contains(key, ":") {
			keyEngine := key[:strings.Index(key, ":")]
			if keyEngine != *engine {
				continue
			}
		}

		candidateProps := r.instanceTypes[key]

		// Must be available in the user's region
		if r.region != "" && !candidateProps.AvailableInRegion(r.region) {
			continue
		}

		// Must be compatible with the user's engine version
		if candidateProps.MinEngineVersion != "" && engineVersion != "" {
			if util.CompareVersions(engineVersion, candidateProps.MinEngineVersion) < 0 {
				continue
			}
		}

		bestKey = key
		bestProps = candidateProps
		bestGen = info.gen
	}

	if bestKey == "" || bestGen == targetInfo.gen {
		return "", types.InstanceProperties{}, false
	}

	return bestKey, bestProps, true
}

// tryUpgradeGeneration attempts to upgrade the target instance to a newer generation.
// If successful, it updates the recommendation's target fields and recalculates cost diff.
// For downscale recommendations, it also re-validates projected CPU and bandwidth constraints.
func (r *RDSRightSize) tryUpgradeRecommendation(
	ctx context.Context,
	rec *types.Recommendation,
	currentProps *types.InstanceProperties,
	instance *rdsTypes.Instance,
	cpuValue *float64,
	bandwidthTotal *float64,
	peakConns *float64,
) {
	if rec.RecommendedInstanceType == nil {
		return
	}

	engineVersion := ""
	if instance.EngineVersion != nil {
		engineVersion = *instance.EngineVersion
	}

	newKey, newProps, found := r.upgradeGeneration(*rec.RecommendedInstanceType, instance.Engine, engineVersion)
	if !found {
		return
	}

	// For downscale: re-validate constraints against the new generation target
	if rec.Recommendation == types.DownScale {
		// Bandwidth constraint
		if newProps.MaxBandwidth != nil && bandwidthTotal != nil {
			if *bandwidthTotal >= float64(*newProps.MaxBandwidth*mbit_bytes) {
				return // newer gen can't handle bandwidth
			}
		}

		// Projected CPU constraint
		if cpuValue != nil && currentProps != nil && currentProps.Vcpu > 0 && newProps.Vcpu > 0 {
			projectedCPU := *cpuValue * float64(currentProps.Vcpu) / float64(newProps.Vcpu)
			if projectedCPU > 100 {
				projectedCPU = 100
			}
			if projectedCPU > r.cpuUpsizeThreshold {
				return // newer gen can't handle CPU
			}
		}
	}

	// For upscale: verify the newer gen target has >= the original target's capacity
	if rec.Recommendation == types.UpScale {
		if rec.TargetInstanceProperties != nil {
			if newProps.Vcpu < rec.TargetInstanceProperties.Vcpu ||
				newProps.Mem < rec.TargetInstanceProperties.Mem {
				return // newer gen has less capacity — don't downgrade
			}
		}
	}

	// Apply the upgrade
	newKeyCopy := stripEnginePrefix(newKey)
	newPropsCopy := newProps
	rec.RecommendedInstanceType = &newKeyCopy
	rec.TargetInstanceProperties = &newPropsCopy
	rec.MonthlyApproximatePriceDiff = Float64((newProps.GetPrice(r.region) - currentProps.GetPrice(r.region)) * hours_month)

	// Re-check connections soft constraint for downscale
	rec.MaxConnectionsAdjustRequired = false
	rec.PeakConnections = nil
	if rec.Recommendation == types.DownScale && peakConns != nil {
		effectiveMax := r.getEffectiveMaxConnections(ctx, instance, &newPropsCopy)
		if effectiveMax != nil && *peakConns >= float64(*effectiveMax) {
			rec.MaxConnectionsAdjustRequired = true
			rec.PeakConnections = peakConns
		}
	}
}

// DoAnalyzeRDS is the original CLI entry point. It runs the analysis and writes
// results to a JSON file and prints cost summary to stdout.
func (r *RDSRightSize) DoAnalyzeRDS() error {
	opts := &AnalysisOptions{
		OnWarning: func(instanceId, msg string) {
			fmt.Fprintf(os.Stderr, "Warning: skipping instance %s: %s\n", instanceId, msg)
		},
	}
	recommendations, err := r.AnalyzeRDS(context.Background(), opts)
	if err != nil {
		return err
	}

	// Stamp region on each recommendation for single-region CLI usage
	for i := range recommendations {
		recommendations[i].Region = r.region
	}

	return WriteResultsCLI(recommendations)
}

// AnalyzeRDS performs the core analysis and returns recommendations as data.
// If opts is nil, defaults are used (no time-series, no progress callback).
func (r *RDSRightSize) AnalyzeRDS(ctx context.Context, opts *AnalysisOptions) ([]types.Recommendation, error) {
	if opts == nil {
		opts = &AnalysisOptions{}
	}

	warn := func(instanceId, msg string) {
		if opts.OnWarning != nil {
			opts.OnWarning(instanceId, msg)
		}
	}

	recommendations := make([]types.Recommendation, 0)
	instances, err := r.rds.GetInstances(ctx)
	if err != nil {
		return nil, err
	}

	// Filter instances by tags first to get accurate total count
	filteredInstances := make([]rdsTypes.Instance, 0)
	for _, instance := range instances {
		requiredTags := r.hasRequiredTags(&instance)
		if *requiredTags {
			filteredInstances = append(filteredInstances, instance)
		}
	}

	total := len(filteredInstances)

	// Collect analysis data for cluster members to enable equalization
	clusterData := make(map[string][]clusterInstanceInfo)

	for i, instance := range filteredInstances {
		if opts.OnProgress != nil {
			opts.OnProgress(i+1, total, *instance.DBInstanceIdentifier)
		}

		metrics, err := r.getMetrics(ctx, &instance)
		if err != nil {
			return nil, err
		}

		// Optionally fetch time-series metrics for graphs
		var tsMetrics *cwTypes.TimeSeriesMetrics
		if opts.FetchTimeSeries {
			tsMetrics, err = r.cloudWatch.GetTimeSeriesMetrics(ctx, instance.DBInstanceIdentifier, r.period, r.statistic)
			if err != nil {
				// Non-fatal: we can still analyze without time-series
				tsMetrics = nil
			}
		}

		// Track data for cluster equalization
		prevRecLen := len(recommendations)
		var instanceProps *types.InstanceProperties
		var cpuValue *float64
		if cpuMetric, hasCPU := metrics.InstanceMetrics[cwTypes.CPUUtilization]; hasCPU && cpuMetric.Value != nil {
			cpuValue = cpuMetric.Value
		}
		peakConns := r.getPeakConnections(metrics)

		noConnections, err := r.hadNoConnections(metrics)
		if err != nil {
			warn(*instance.DBInstanceIdentifier, err.Error())
			continue
		}

		if *noConnections {
			terminateRec := types.Recommendation{
				Instance:          instance,
				Recommendation:    types.Terminate,
				Reason:            types.NoUsageWithinPeriodReason,
				TimeSeriesMetrics: tsMetrics,
			}
			// Look up current instance properties so we can compute the cost of termination
			if termProps, ok := r.lookupInstanceProperties(*instance.DBInstanceClass, instance.Engine); ok {
				instanceProps = &termProps
				terminateRec.CurrentInstanceProperties = &termProps
				// Terminating saves the full current cost (target cost is $0)
				terminateRec.MonthlyApproximatePriceDiff = Float64(-termProps.GetPrice(r.region) * hours_month)
			}
			recommendations = append(recommendations, terminateRec)
		} else {
			instanceProperties, mappedInstance := r.lookupInstanceProperties(*instance.DBInstanceClass, instance.Engine)

			if mappedInstance {
				instanceProps = &instanceProperties

				memoryUtilization, err := r.getMemoryUtilization(metrics, &instanceProperties)
				if err != nil {
					warn(*instance.DBInstanceIdentifier, err.Error())
					continue
				}

			if *memoryUtilization.UnderProvisioned && instanceProperties.Up != nil {
				upInstance, _ := r.lookupInstanceProperties(*instanceProperties.Up, instance.Engine)
				upName := stripEnginePrefix(*instanceProperties.Up)
				recommendations = append(recommendations, types.Recommendation{
					Instance:                    instance,
					Recommendation:              types.UpScale,
					Reason:                      types.MemoryUnderProvisionedReason,
					RecommendedInstanceType:     &upName,
						MetricValue:                 memoryUtilization.Value,
						MonthlyApproximatePriceDiff: Float64((upInstance.GetPrice(r.region) - instanceProperties.GetPrice(r.region)) * hours_month),
						CurrentInstanceProperties:   &instanceProperties,
						TargetInstanceProperties:    &upInstance,
						TimeSeriesMetrics:           tsMetrics,
					})

					// Try upgrading to a newer instance generation
					if r.preferNewGen {
						r.tryUpgradeRecommendation(
							ctx,
							&recommendations[len(recommendations)-1],
							&instanceProperties, &instance, cpuValue, nil, peakConns,
						)
					}
				} else {
					cpuUtilization, err := r.getCPUUtilization(metrics)
					if err != nil {
						warn(*instance.DBInstanceIdentifier, err.Error())
						continue
					}

					bandwidthUtilization, err := r.getBandwidthUtilization(metrics, &instanceProperties)
					if err != nil {
						warn(*instance.DBInstanceIdentifier, err.Error())
						continue
					}

				if cpuUtilization.Status == types.CPUUnderProvisioned && instanceProperties.Up != nil {
					upInstance, _ := r.lookupInstanceProperties(*instanceProperties.Up, instance.Engine)
					cpuUpName := stripEnginePrefix(*instanceProperties.Up)
					recommendations = append(recommendations, types.Recommendation{
						Instance:                    instance,
						Recommendation:              types.UpScale,
						Reason:                      types.CPUUnderProvisionedReason,
						RecommendedInstanceType:     &cpuUpName,
							MetricValue:                 cpuUtilization.Value,
							MonthlyApproximatePriceDiff: Float64((upInstance.GetPrice(r.region) - instanceProperties.GetPrice(r.region)) * hours_month),
							CurrentInstanceProperties:   &instanceProperties,
							TargetInstanceProperties:    &upInstance,
							TimeSeriesMetrics:           tsMetrics,
						})

						// Try upgrading to a newer instance generation
						if r.preferNewGen {
							r.tryUpgradeRecommendation(
								ctx,
								&recommendations[len(recommendations)-1],
								&instanceProperties, &instance, cpuValue, nil, peakConns,
							)
						}
					} else if cpuUtilization.Status == types.CPUOverProvisioned && bandwidthUtilization.Status != types.BandwidthUnderProvisioned && instanceProperties.Down != nil {
						// Walk down the instance chain to find the optimal (smallest) downscale target
						// where projected CPU stays within acceptable bounds.

						var bestDown *string
						var bestDownInstance *types.InstanceProperties

						candidateName := instanceProperties.Down
						for candidateName != nil {
							candidate, exists := r.lookupInstanceProperties(*candidateName, instance.Engine)
							if !exists {
								break
							}

							// Skip candidates not available in the user's region
							if r.region != "" && !candidate.AvailableInRegion(r.region) {
								candidateName = candidate.Down
								continue
							}

							// Hard constraint: bandwidth — target must handle current throughput
							if candidate.MaxBandwidth == nil || *bandwidthUtilization.Total >= float64(*candidate.MaxBandwidth*mbit_bytes) {
								break
							}

							// Hard constraint: projected CPU must not exceed upsize threshold
							projectedCPU := *cpuUtilization.Value * float64(instanceProperties.Vcpu) / float64(candidate.Vcpu)
							if projectedCPU > 100 {
								projectedCPU = 100
							}
							if projectedCPU > r.cpuUpsizeThreshold {
								break
							}

						// Valid candidate — record as best so far
						candidateCopy := candidate
						strippedCandidate := stripEnginePrefix(*candidateName)
						bestDown = &strippedCandidate
						bestDownInstance = &candidateCopy

							// If projected CPU is in the optimized zone, this is the ideal target
							if projectedCPU >= r.cpuDownsizeThreshold {
								break
							}

							// Still over-provisioned on this candidate — try even smaller
							candidateName = candidate.Down
						}

						if bestDown != nil && bestDownInstance != nil {
							rec := types.Recommendation{
								Instance:                    instance,
								Recommendation:              types.DownScale,
								Reason:                      types.CPUOverProvisionedReason,
								RecommendedInstanceType:     bestDown,
								MetricValue:                 cpuUtilization.Value,
								MonthlyApproximatePriceDiff: Float64((bestDownInstance.GetPrice(r.region) - instanceProperties.GetPrice(r.region)) * hours_month),
								CurrentInstanceProperties:   &instanceProperties,
								TargetInstanceProperties:    bestDownInstance,
								TimeSeriesMetrics:           tsMetrics,
							}

							// Soft constraint: connections warning
							if peakConns != nil {
								effectiveMax := r.getEffectiveMaxConnections(ctx, &instance, bestDownInstance)
								if effectiveMax != nil && *peakConns >= float64(*effectiveMax) {
									rec.MaxConnectionsAdjustRequired = true
									rec.PeakConnections = peakConns
								}
							}

							recommendations = append(recommendations, rec)

							// Try upgrading to a newer instance generation
							if r.preferNewGen {
								r.tryUpgradeRecommendation(
									ctx,
									&recommendations[len(recommendations)-1],
									&instanceProperties, &instance, cpuValue, bandwidthUtilization.Total, peakConns,
								)
							}
						}
					}
				}
			}
		}

		// Collect cluster data for equalization
		if instance.DBClusterIdentifier != nil && *instance.DBClusterIdentifier != "" {
			clusterID := *instance.DBClusterIdentifier
			recIdx := -1
			if len(recommendations) > prevRecLen {
				recIdx = len(recommendations) - 1
			}
			// Look up properties if not already captured (e.g., Terminate instances)
			props := instanceProps
			if props == nil {
				if p, ok := r.lookupInstanceProperties(*instance.DBInstanceClass, instance.Engine); ok {
					props = &p
				}
			}
			clusterData[clusterID] = append(clusterData[clusterID], clusterInstanceInfo{
				instance:   instance,
				properties: props,
				cpuValue:   cpuValue,
				peakConns:  peakConns,
				tsMetrics:  tsMetrics,
				recIndex:   recIdx,
			})
		}
	}

	// Equalize recommendations within clusters
	recommendations = r.equalizeClusterRecommendations(ctx, recommendations, clusterData)

	// Sort recommendations: cluster members grouped together, then by instance ID
	SortRecommendations(recommendations)

	// Compute projected CPU for all non-Terminate recommendations
	for i := range recommendations {
		rec := &recommendations[i]
		if rec.Recommendation != types.Terminate &&
			rec.MetricValue != nil &&
			rec.CurrentInstanceProperties != nil &&
			rec.TargetInstanceProperties != nil &&
			rec.CurrentInstanceProperties.Vcpu > 0 &&
			rec.TargetInstanceProperties.Vcpu > 0 {

			projected := *rec.MetricValue * float64(rec.CurrentInstanceProperties.Vcpu) / float64(rec.TargetInstanceProperties.Vcpu)
			if projected > 100 {
				projected = 100
			}
			rec.ProjectedCPU = &projected
		}
	}

	return recommendations, nil
}

// equalizeClusterRecommendations adjusts recommendations so all instances in the same
// Aurora cluster share a single target instance type. The cluster target is the largest
// (by vCPU, then memory) among all members' ideal types:
//   - Instances with UPSCALE/DOWNSCALE recommendations: their recommended target
//   - Optimized instances (no recommendation): their current instance type
//   - TERMINATE instances (in non-all-terminate clusters): their current instance type
func (r *RDSRightSize) equalizeClusterRecommendations(
	ctx context.Context,
	recommendations []types.Recommendation,
	clusterData map[string][]clusterInstanceInfo,
) []types.Recommendation {
	removeIndices := make(map[int]bool)
	var newRecs []types.Recommendation

	for _, members := range clusterData {
		if len(members) <= 1 {
			continue
		}

		// Check if ALL members with recommendations are TERMINATE
		allTerminate := true
		for _, m := range members {
			if m.recIndex >= 0 {
				if recommendations[m.recIndex].Recommendation != types.Terminate {
					allTerminate = false
					break
				}
			} else {
				// No recommendation means optimized — not terminate
				allTerminate = false
				break
			}
		}
		if allTerminate {
			continue
		}

		// Determine each member's "ideal" instance type and find the cluster target
		// (the largest by vCPU, then memory)
		var clusterTarget string
		var clusterTargetProps types.InstanceProperties

		for _, m := range members {
			var idealType string

			if m.recIndex >= 0 {
				rec := &recommendations[m.recIndex]
				if rec.Recommendation == types.Terminate {
					// In a non-all-terminate cluster, treat as wanting current size
					if rec.DBInstanceClass != nil {
						idealType = *rec.DBInstanceClass
					}
				} else if rec.RecommendedInstanceType != nil {
					idealType = *rec.RecommendedInstanceType
				}
			} else {
				// Optimized instance — ideal is current size
				if m.instance.DBInstanceClass != nil {
					idealType = *m.instance.DBInstanceClass
				}
			}

			if idealType == "" {
				continue
			}

			idealProps, exists := r.lookupInstanceProperties(idealType, m.instance.Engine)
			if !exists {
				continue
			}

			if clusterTarget == "" ||
				idealProps.Vcpu > clusterTargetProps.Vcpu ||
				(idealProps.Vcpu == clusterTargetProps.Vcpu && idealProps.Mem > clusterTargetProps.Mem) {
				clusterTarget = idealType
				clusterTargetProps = idealProps
			}
		}

		if clusterTarget == "" {
			continue
		}

		// Only upgrade generation if at least one member has a scaling recommendation.
		// Don't create generation-only changes for clusters where all instances are optimized.
		hasScalingRec := false
		for _, m := range members {
			if m.recIndex >= 0 {
				rec := recommendations[m.recIndex]
				if rec.Recommendation == types.UpScale || rec.Recommendation == types.DownScale {
					hasScalingRec = true
					break
				}
			}
		}

		// Try upgrading the cluster target to a newer instance generation
		if r.preferNewGen && hasScalingRec {
			// Use the engine and version from the first member (all members in a cluster share the same engine/version)
			var clusterEngine *string
			var clusterEngineVersion string
			for _, m := range members {
				if m.instance.Engine != nil {
					clusterEngine = m.instance.Engine
				}
				if m.instance.EngineVersion != nil {
					clusterEngineVersion = *m.instance.EngineVersion
				}
				if clusterEngine != nil {
					break
				}
			}
			if newKey, newProps, found := r.upgradeGeneration(clusterTarget, clusterEngine, clusterEngineVersion); found {
				// Only upgrade if the new gen has at least the same capacity
				if newProps.Vcpu >= clusterTargetProps.Vcpu && newProps.Mem >= clusterTargetProps.Mem {
					clusterTarget = newKey
					clusterTargetProps = newProps
				}
			}
		}

		// Apply equalization to each member
		for _, m := range members {
			currentType := ""
			if m.instance.DBInstanceClass != nil {
				currentType = *m.instance.DBInstanceClass
			}

			// If target equals current instance type, no recommendation needed
			if sameInstanceClass(clusterTarget, currentType) {
				if m.recIndex >= 0 {
					removeIndices[m.recIndex] = true
				}
				continue
			}

			currentProps := m.properties
			if currentProps == nil {
				continue
			}

			// Determine direction by comparing cluster target to current instance
			var recType types.RecommendationType
			if clusterTargetProps.Vcpu > currentProps.Vcpu ||
				(clusterTargetProps.Vcpu == currentProps.Vcpu && clusterTargetProps.Mem > currentProps.Mem) {
				recType = types.UpScale
			} else {
				recType = types.DownScale
			}

			targetName := stripEnginePrefix(clusterTarget)
			targetPropsCopy := clusterTargetProps

			if m.recIndex >= 0 {
				// Update existing recommendation
				rec := &recommendations[m.recIndex]

				// If target already matches, no equalization needed
				if rec.RecommendedInstanceType != nil && sameInstanceClass(*rec.RecommendedInstanceType, clusterTarget) {
					continue
				}

				rec.Recommendation = recType
				rec.RecommendedInstanceType = &targetName
				rec.CurrentInstanceProperties = currentProps
				rec.TargetInstanceProperties = &targetPropsCopy
				rec.MonthlyApproximatePriceDiff = Float64((targetPropsCopy.GetPrice(r.region) - currentProps.GetPrice(r.region)) * hours_month)
				rec.ClusterEqualized = true

				// Set MetricValue to CPU for projected CPU calculation if not already set
				if rec.MetricValue == nil && m.cpuValue != nil {
					rec.MetricValue = m.cpuValue
				}

				// Override TERMINATE reason
				if rec.Reason == types.NoUsageWithinPeriodReason {
					rec.Reason = types.ClusterEqualizationReason
				}

				// Recalculate connections warning
				rec.MaxConnectionsAdjustRequired = false
				rec.PeakConnections = nil
				if m.peakConns != nil && recType == types.DownScale {
					effectiveMax := r.getEffectiveMaxConnections(ctx, &m.instance, &targetPropsCopy)
					if effectiveMax != nil && *m.peakConns >= float64(*effectiveMax) {
						rec.MaxConnectionsAdjustRequired = true
						rec.PeakConnections = m.peakConns
					}
				}
			} else {
				// Create new recommendation for optimized instance
				rec := types.Recommendation{
					Instance:                    m.instance,
					Recommendation:              recType,
					Reason:                      types.ClusterEqualizationReason,
					RecommendedInstanceType:     &targetName,
					MetricValue:                 m.cpuValue,
					MonthlyApproximatePriceDiff: Float64((targetPropsCopy.GetPrice(r.region) - currentProps.GetPrice(r.region)) * hours_month),
					CurrentInstanceProperties:   currentProps,
					TargetInstanceProperties:    &targetPropsCopy,
					TimeSeriesMetrics:           m.tsMetrics,
					ClusterEqualized:            true,
				}

				// Connections warning for downscale
				if m.peakConns != nil && recType == types.DownScale {
					effectiveMax := r.getEffectiveMaxConnections(ctx, &m.instance, &targetPropsCopy)
					if effectiveMax != nil && *m.peakConns >= float64(*effectiveMax) {
						rec.MaxConnectionsAdjustRequired = true
						rec.PeakConnections = m.peakConns
					}
				}

				newRecs = append(newRecs, rec)
			}
		}
	}

	// Remove recommendations where cluster target == current instance type
	if len(removeIndices) > 0 {
		filtered := make([]types.Recommendation, 0, len(recommendations)-len(removeIndices))
		for i, rec := range recommendations {
			if !removeIndices[i] {
				filtered = append(filtered, rec)
			}
		}
		recommendations = filtered
	}

	// Add new recommendations for optimized instances pulled into cluster changes
	recommendations = append(recommendations, newRecs...)

	return recommendations
}

// CalculateCostDifference computes the total monthly cost difference across all recommendations.
func CalculateCostDifference(recommendations []types.Recommendation) float64 {
	var priceDiff float64 = 0
	for _, recommendation := range recommendations {
		if recommendation.MonthlyApproximatePriceDiff != nil {
			priceDiff = priceDiff + *recommendation.MonthlyApproximatePriceDiff
		}
	}
	return priceDiff
}

// CostBreakdown separates scaling (upscale/downscale) costs from terminate costs.
type CostBreakdown struct {
	ScalingMonthly  float64 // UPSCALE + DOWNSCALE price diffs only
	TotalMonthly    float64 // All recommendations including TERMINATE
	HasTerminations bool    // Whether any TERMINATE recs contributed
}

// Yearly returns the yearly equivalents.
func (cb CostBreakdown) ScalingYearly() float64 { return cb.ScalingMonthly * 12 }
func (cb CostBreakdown) TotalYearly() float64   { return cb.TotalMonthly * 12 }

// CalculateCostBreakdown computes monthly cost differences split by scaling vs terminate.
func CalculateCostBreakdown(recommendations []types.Recommendation) CostBreakdown {
	var cb CostBreakdown
	for _, rec := range recommendations {
		if rec.MonthlyApproximatePriceDiff == nil {
			continue
		}
		diff := *rec.MonthlyApproximatePriceDiff
		cb.TotalMonthly += diff
		if rec.Recommendation == types.Terminate {
			cb.HasTerminations = true
		} else {
			cb.ScalingMonthly += diff
		}
	}
	return cb
}

// CalculateRegionalCostBreakdown computes cost breakdowns grouped by region.
// Returns a map of region -> CostBreakdown, and a sorted slice of region names.
func CalculateRegionalCostBreakdown(recommendations []types.Recommendation) (map[string]CostBreakdown, []string) {
	byRegion := make(map[string]CostBreakdown)
	for _, rec := range recommendations {
		region := rec.Region
		if region == "" {
			continue
		}
		cb := byRegion[region]
		if rec.MonthlyApproximatePriceDiff != nil {
			diff := *rec.MonthlyApproximatePriceDiff
			cb.TotalMonthly += diff
			if rec.Recommendation == types.Terminate {
				cb.HasTerminations = true
			} else {
				cb.ScalingMonthly += diff
			}
		}
		byRegion[region] = cb
	}

	regions := make([]string, 0, len(byRegion))
	for r := range byRegion {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	return byRegion, regions
}

// SortRecommendations sorts recommendations so Aurora cluster members are grouped together
// and then ordered by instance ID within each group.
func SortRecommendations(recs []types.Recommendation) {
	sort.SliceStable(recs, func(i, j int) bool {
		ci := recs[i].DBClusterIdentifier
		cj := recs[j].DBClusterIdentifier
		if ci != nil && cj == nil {
			return true
		}
		if ci == nil && cj != nil {
			return false
		}
		if ci != nil && cj != nil && *ci != *cj {
			return *ci < *cj
		}
		ii := recs[i].DBInstanceIdentifier
		ij := recs[j].DBInstanceIdentifier
		if ii != nil && ij != nil {
			return *ii < *ij
		}
		return false
	})
}

// WriteResultsCLI prints cost summary to stdout and writes the JSON file.
// This is used by both single-region DoAnalyzeRDS and multi-region CLI orchestration.
func WriteResultsCLI(recommendations []types.Recommendation) error {
	writeApproximateCostDifference(recommendations)
	absPath, err := WriteRecommendationsJSON(recommendations)
	if err != nil {
		return err
	}
	fmt.Println(absPath)
	return nil
}
func WriteRecommendationsJSON(recommendations []types.Recommendation) (string, error) {
	data, err := json.MarshalIndent(recommendations, "", "  ")
	if err != nil {
		return "", err
	}

	const layout = "2006-01-02_15-04-05"
	t := time.Now()
	filename := "recommendations-" + t.Format(layout) + ".json"
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(filename)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

func Float64(v float64) *float64 {
	return ptr.Float64(v)
}

func writeApproximateCostDifference(recommendations []types.Recommendation) {
	cb := CalculateCostBreakdown(recommendations)

	formatLine := func(label string, monthly float64) string {
		if monthly > 0 {
			return fmt.Sprintf("%s: price increase of approximately $%.2f/month ($%.2f/year)", label, monthly, monthly*12)
		} else if monthly < 0 {
			savings := monthly * -1
			return fmt.Sprintf("%s: savings of approximately $%.2f/month ($%.2f/year)", label, savings, savings*12)
		}
		return ""
	}

	if cb.HasTerminations && cb.ScalingMonthly != cb.TotalMonthly {
		if scalingLine := formatLine("Scaling changes", cb.ScalingMonthly); scalingLine != "" {
			fmt.Println(scalingLine)
		} else {
			fmt.Println("Scaling changes: no cost impact")
		}
		if totalLine := formatLine("Total (w/ terminations)", cb.TotalMonthly); totalLine != "" {
			fmt.Println(totalLine)
		}
	} else {
		if cb.TotalMonthly > 0 {
			fmt.Println(fmt.Sprintf("The changes will yield a price increase of approximately $%.2f/month ($%.2f/year)", cb.TotalMonthly, cb.TotalMonthly*12))
		} else if cb.TotalMonthly < 0 {
			savings := cb.TotalMonthly * -1
			fmt.Println(fmt.Sprintf("The changes will yield a savings of approximately $%.2f/month ($%.2f/year)", savings, savings*12))
		}
	}

	// Per-region breakdown when multiple regions are present
	regionalCB, regions := CalculateRegionalCostBreakdown(recommendations)
	if len(regions) > 1 {
		for _, region := range regions {
			rcb := regionalCB[region]
			if rcb.TotalMonthly > 0 {
				fmt.Printf("  %s: price increase of approximately $%.2f/month ($%.2f/year)\n", region, rcb.TotalMonthly, rcb.TotalMonthly*12)
			} else if rcb.TotalMonthly < 0 {
				savings := rcb.TotalMonthly * -1
				fmt.Printf("  %s: savings of approximately $%.2f/month ($%.2f/year)\n", region, savings, savings*12)
			} else {
				fmt.Printf("  %s: no cost impact\n", region)
			}
		}
	}
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
		return nil, errors.New("no database connections metric found for instance " + *metrics.DBInstanceIdentifier)
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

func (r *RDSRightSize) getMetrics(ctx context.Context, instance *rdsTypes.Instance) (*cwTypes.Metrics, error) {
	return r.cloudWatch.GetMetrics(ctx, instance.DBInstanceIdentifier, r.period, r.statistic)
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

// getPeakConnections extracts the peak (Maximum) database connections value from metrics.
func (r *RDSRightSize) getPeakConnections(metrics *cwTypes.Metrics) *float64 {
	metric, ok := metrics.InstanceMetrics[cwTypes.DatabaseConnections]
	if !ok || metric.Value == nil {
		return nil
	}
	return metric.Value
}

// getEffectiveMaxConnections determines the effective max_connections limit for the target instance.
// It tries the parameter group API first (cached), falling back to the JSON-defined value.
func (r *RDSRightSize) getEffectiveMaxConnections(ctx context.Context, instance *rdsTypes.Instance, targetProperties *types.InstanceProperties) *int64 {
	targetMax := targetProperties.MaxConnections

	// Try to get the current instance's configured max_connections from its parameter group
	if instance.DBParameterGroupName != nil && *instance.DBParameterGroupName != "" {
		pgName := *instance.DBParameterGroupName

		// Check cache first
		if cached, found := r.maxConnCache[pgName]; found {
			if cached != nil && targetMax != nil && *cached < *targetMax {
				return cached
			}
			return targetMax
		}

		// Query the API
		apiMax, err := r.rds.GetMaxConnections(ctx, instance.DBParameterGroupName)
		r.maxConnCache[pgName] = apiMax // cache even nil results

		if err == nil && apiMax != nil {
			// If the user has set a static max_connections lower than the target's default,
			// use the user's value (it will remain the same after resize if same param group)
			if targetMax != nil && *apiMax < *targetMax {
				return apiMax
			}
		}
	}

	return targetMax
}

func loadInstanceTypes(source *string) types.InstanceTypes {
	if strings.HasPrefix(*source, "http://") || strings.HasPrefix(*source, "https://") {
		return loadInstanceTypesFromURL(*source)
	}

	// Treat as local file path (strip optional file:// prefix)
	path := strings.TrimPrefix(*source, "file://")
	return loadInstanceTypesFromFile(path)
}

func loadInstanceTypesFromFile(path string) types.InstanceTypes {
	body, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to read instance types file %s: %v", path, err))
	}

	instanceTypes := types.InstanceTypes{}
	jsonErr := json.Unmarshal(body, &instanceTypes)
	if jsonErr != nil {
		log.Fatal(fmt.Sprintf("Failed to parse instance types JSON from %s: %v", path, jsonErr))
	}

	return instanceTypes
}

func loadInstanceTypesFromURL(url string) types.InstanceTypes {
	httpClient := http.Client{
		Timeout: time.Second * 10,
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)

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
