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
	namespace     = "AWS/RDS"
	dimensionName = "DBInstanceIdentifier"
)

type CloudWatch struct {
	cwClient *cloudwatch.Client
}

func NewCloudWatch(awsConfig *aws.Config) *CloudWatch {
	return &CloudWatch{
		cwClient: cloudwatch.NewFromConfig(*awsConfig),
	}
}

func (c *CloudWatch) GetMetric(dbInstanceId *string, periodInDays int, metric types.RdsMetricName) (*types.Metric, error) {

	endTime := time.Now().Truncate(time.Hour)
	startTime := endTime.AddDate(0, 0, (periodInDays)*-1)

	period := int32(periodInDays * 24 * 60 * 60)

	input := &cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String(namespace),
		MetricName: aws.String(metric.String()),
		Dimensions: []cwTypes.Dimension{
			{
				Name:  aws.String(dimensionName),
				Value: dbInstanceId,
			},
		},
		StartTime: aws.Time(startTime),
		EndTime:   aws.Time(endTime),
		Period:    aws.Int32(period),
		Statistics: []cwTypes.Statistic{
			cwTypes.StatisticMaximum,
			cwTypes.StatisticAverage,
			cwTypes.StatisticMinimum,
		},
	}

	output, err := c.cwClient.GetMetricStatistics(context.TODO(), input)

	if err != nil {
		return nil, err
	}

	var m types.Metric
	for _, datapoint := range output.Datapoints {
		m = types.Metric{
			DBInstanceIdentifier: dbInstanceId,
			Maximum:              datapoint.Maximum,
			Minimum:              datapoint.Minimum,
			Average:              datapoint.Average,
			Unit:                 datapoint.Unit,
		}
	}

	return &m, nil
}
