package types

type RdsMetricName string
type StatName string

// Enum values for RDS Metrics
const (
	CPUUtilization      RdsMetricName = "CPUUtilization"
	DatabaseConnections RdsMetricName = "DatabaseConnections"
	FreeableMemory      RdsMetricName = "FreeableMemory"
	WriteThroughput     RdsMetricName = "WriteThroughput"
	ReadThroughput      RdsMetricName = "ReadThroughput"
	Average             StatName      = "Average"
	P99                 StatName      = "p99"
	P98                 StatName      = "p98"
	P95                 StatName      = "p95"
	P50                 StatName      = "p50"
)

func (c RdsMetricName) String() string {
	return string(c)
}

func (c StatName) String() string {
	return string(c)
}

type Metrics struct {
	DBInstanceIdentifier *string
	InstanceMetrics      map[RdsMetricName]Metric
}

type Metric struct {
	Value *float64
}
