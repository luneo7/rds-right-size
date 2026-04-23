package types

import (
	cwTypes "github.com/luneo7/rds-right-size/internal/cw/types"
	rdsTypes "github.com/luneo7/rds-right-size/internal/rds/types"
)

type CPUUtilizationStatus string
type BandwidthUtilizationStatus string
type RecommendationType string
type RecommendationReason string

// Enum values for CPU Utilization Status
const (
	CPUOptimized                 CPUUtilizationStatus       = "CPUOptimized"
	CPUOverProvisioned           CPUUtilizationStatus       = "CPUOverProvisioned"
	CPUUnderProvisioned          CPUUtilizationStatus       = "CPUUnderProvisioned"
	BandwidthOptimized           BandwidthUtilizationStatus = "BandwidthOptimized"
	BandwidthOverProvisioned     BandwidthUtilizationStatus = "BandwidthOverProvisioned"
	BandwidthUnderProvisioned    BandwidthUtilizationStatus = "BandwidthUnderProvisioned"
	UpScale                      RecommendationType         = "UpScale"
	DownScale                    RecommendationType         = "DownScale"
	Terminate                    RecommendationType         = "Terminate"
	NoUsageWithinPeriodReason    RecommendationReason       = "No usage within period"
	MemoryUnderProvisionedReason RecommendationReason       = "Memory is under provisioned"
	CPUUnderProvisionedReason    RecommendationReason       = "CPU is under provisioned"
	CPUOverProvisionedReason     RecommendationReason       = "CPU is over provisioned"
	ClusterEqualizationReason    RecommendationReason       = "Cluster equalization"
)

type CPUUtilization struct {
	Value  *float64
	Status CPUUtilizationStatus
}

type BandwidthUtilization struct {
	Value  *float64
	Total  *float64
	Status BandwidthUtilizationStatus
}

type MemoryUtilization struct {
	Value            *float64
	UnderProvisioned *bool
}

type InstanceTypes map[string]InstanceProperties

type InstanceProperties struct {
	Vcpu             int64              `json:"vcpu"`
	Up               *string            `json:"up"`
	Down             *string            `json:"down"`
	Mem              int64              `json:"mem"`
	MaxBandwidth     *int64             `json:"maxBandwidth"`
	MaxConnections   *int64             `json:"maxConnections"`
	Pricing          map[string]float64 `json:"pricing,omitempty"`
	MinEngineVersion string             `json:"minEngineVersion,omitempty"`
	StdPrice         float64            `json:"stdPrice,omitempty"`
}

// GetPrice returns the on-demand hourly price for the given region.
// If multi-region pricing is available, it uses the pricing map.
// Otherwise it falls back to StdPrice for backward compatibility with old JSON files.
func (p InstanceProperties) GetPrice(region string) float64 {
	if p.Pricing != nil {
		if price, ok := p.Pricing[region]; ok {
			return price
		}
	}
	return p.StdPrice
}

// AvailableInRegion returns true if this instance type is available in the given region.
// If the pricing map is nil (old format JSON), it assumes availability (backward compat).
func (p InstanceProperties) AvailableInRegion(region string) bool {
	if p.Pricing == nil {
		return true // old format JSON, assume available
	}
	_, ok := p.Pricing[region]
	return ok
}

type Recommendation struct {
	rdsTypes.Instance
	Region                       string `json:"Region,omitempty"`
	Recommendation               RecommendationType
	Reason                       RecommendationReason
	RecommendedInstanceType      *string
	MetricValue                  *float64
	ProjectedCPU                 *float64 `json:"ProjectedCPU,omitempty"`
	MaxConnectionsAdjustRequired bool     `json:"MaxConnectionsAdjustRequired,omitempty"`
	PeakConnections              *float64 `json:"PeakConnections,omitempty"`
	ClusterEqualized             bool     `json:"ClusterEqualized,omitempty"`
	MonthlyApproximatePriceDiff  *float64
	CurrentInstanceProperties    *InstanceProperties        `json:"-"`
	TargetInstanceProperties     *InstanceProperties        `json:"-"`
	TimeSeriesMetrics            *cwTypes.TimeSeriesMetrics `json:"-"`
}
