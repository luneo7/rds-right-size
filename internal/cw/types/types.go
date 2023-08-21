package types

import (
	cwTypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

type RdsMetricName string

// Enum values for RDS Metrics
const (
	CPUUtilization      RdsMetricName = "CPUUtilization"
	DatabaseConnections RdsMetricName = "DatabaseConnections"
	FreeableMemory      RdsMetricName = "FreeableMemory"
)

func (c RdsMetricName) String() string {
	return string(c)
}

type Metric struct {
	// The identifier for the source DB instance, which can't be changed and which is
	// unique to an Amazon Web Services Region.
	DBInstanceIdentifier *string

	// The standard unit for the data point.
	Unit cwTypes.StandardUnit

	// The maximum metric value.
	Maximum *float64

	// The minimum metric value.
	Minimum *float64

	// The average of the metric values.
	Average *float64
}
