package types

import (
	rdsTypes "github.com/luneo7/go-rds-right-size/internal/rds/types"
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
	Vcpu         int64   `json:"vcpu"`
	Up           *string `json:"up"`
	Down         *string `json:"down"`
	Mem          int64   `json:"mem"`
	MaxBandwidth *int64  `json:"maxBandwidth"`
	StdPrice     float64 `json:"stdPrice"`
}

type Recommendation struct {
	rdsTypes.Instance
	Recommendation              RecommendationType
	Reason                      RecommendationReason
	RecommendedInstanceType     *string
	MetricValue                 *float64
	MonthlyApproximatePriceDiff *float64
}
