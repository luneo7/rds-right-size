package types

type RdsMetricName string

// Enum values for RDS Metrics
const (
	CPUUtilization      RdsMetricName = "CPUUtilization"
	DatabaseConnections RdsMetricName = "DatabaseConnections"
	FreeableMemory      RdsMetricName = "FreeableMemory"
	WriteThroughput     RdsMetricName = "WriteThroughput"
	ReadThroughput      RdsMetricName = "ReadThroughput"
)

func (c RdsMetricName) String() string {
	return string(c)
}

type Metrics struct {
	DBInstanceIdentifier *string
	InstanceMetrics      map[RdsMetricName]Metric
}

type Metric struct {
	Value *float64
}
