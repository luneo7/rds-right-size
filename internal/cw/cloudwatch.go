package cw

import (
	"context"
	"github.com/luneo7/go-rds-right-size/internal/cw/types"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwTypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

const (
	namespace             = "AWS/RDS"
	dimensionName         = "DBInstanceIdentifier"
	cpuUtilizationId      = "cpu"
	databaseConnectionsId = "connections"
	freeableMemoryId      = "freeablemem"
	writeThroughputId     = "write"
	readThroughputId      = "read"
)

type CloudWatch struct {
	cwClient *cloudwatch.Client
}

func NewCloudWatch(awsConfig *aws.Config) *CloudWatch {
	return &CloudWatch{
		cwClient: cloudwatch.NewFromConfig(*awsConfig),
	}
}

func (c *CloudWatch) GetMetrics(dbInstanceId *string, periodInDays int, statistic types.StatName) (*types.Metrics, error) {

	endTime := time.Now().UTC().Truncate(time.Hour)
	startTime := endTime.AddDate(0, 0, (periodInDays)*-1)

	period := int32(periodInDays * 24 * 60 * 60)

	input := &cloudwatch.GetMetricDataInput{
		StartTime: aws.Time(startTime),
		EndTime:   aws.Time(endTime),
		MetricDataQueries: []cwTypes.MetricDataQuery{
			{
				Id: aws.String(databaseConnectionsId),
				MetricStat: &cwTypes.MetricStat{
					Metric: &cwTypes.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(types.DatabaseConnections.String()),
						Dimensions: []cwTypes.Dimension{
							{
								Name:  aws.String(dimensionName),
								Value: dbInstanceId,
							},
						},
					},
					Period: &period,
					Stat:   aws.String(types.Average.String()),
				},
			},
			{
				Id: aws.String(freeableMemoryId),
				MetricStat: &cwTypes.MetricStat{
					Metric: &cwTypes.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(types.FreeableMemory.String()),
						Dimensions: []cwTypes.Dimension{
							{
								Name:  aws.String(dimensionName),
								Value: dbInstanceId,
							},
						},
					},
					Period: &period,
					Stat:   aws.String(statistic.String()),
				},
			},
			{
				Id: aws.String(cpuUtilizationId),
				MetricStat: &cwTypes.MetricStat{
					Metric: &cwTypes.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(types.CPUUtilization.String()),
						Dimensions: []cwTypes.Dimension{
							{
								Name:  aws.String(dimensionName),
								Value: dbInstanceId,
							},
						},
					},
					Period: &period,
					Stat:   aws.String(statistic.String()),
				},
			},
			{
				Id: aws.String(writeThroughputId),
				MetricStat: &cwTypes.MetricStat{
					Metric: &cwTypes.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(types.WriteThroughput.String()),
						Dimensions: []cwTypes.Dimension{
							{
								Name:  aws.String(dimensionName),
								Value: dbInstanceId,
							},
						},
					},
					Period: &period,
					Stat:   aws.String(statistic.String()),
				},
			},
			{
				Id: aws.String(readThroughputId),
				MetricStat: &cwTypes.MetricStat{
					Metric: &cwTypes.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(types.ReadThroughput.String()),
						Dimensions: []cwTypes.Dimension{
							{
								Name:  aws.String(dimensionName),
								Value: dbInstanceId,
							},
						},
					},
					Period: &period,
					Stat:   aws.String(statistic.String()),
				},
			},
		},
	}

	output, err := c.cwClient.GetMetricData(context.Background(), input)

	if err != nil {
		return nil, err
	}

	var m types.Metrics
	metrics := make(map[types.RdsMetricName]types.Metric)

	for _, result := range output.MetricDataResults {
		var metricName types.RdsMetricName

		switch *result.Id {
		case databaseConnectionsId:
			metricName = types.DatabaseConnections
		case freeableMemoryId:
			metricName = types.FreeableMemory
		case cpuUtilizationId:
			metricName = types.CPUUtilization
		case writeThroughputId:
			metricName = types.WriteThroughput
		case readThroughputId:
			metricName = types.ReadThroughput
		}

		for _, value := range result.Values {
			metrics[metricName] = types.Metric{
				Value: &value,
			}
		}
	}

	m = types.Metrics{
		DBInstanceIdentifier: dbInstanceId,
		InstanceMetrics:      metrics,
	}

	return &m, nil
}
