package rds

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsRds "github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/luneo7/go-rds-right-size/internal/rds/types"
)

type RDS struct {
	rdsClient *awsRds.Client
}

func NewRDS(awsConfig *aws.Config) *RDS {
	return &RDS{
		rdsClient: awsRds.NewFromConfig(*awsConfig),
	}
}

func (r *RDS) GetInstances() ([]types.Instance, error) {
	var output *awsRds.DescribeDBInstancesOutput
	var err error
	var dbInstances []types.Instance

	paginator := awsRds.NewDescribeDBInstancesPaginator(r.rdsClient, &awsRds.DescribeDBInstancesInput{})

	for paginator.HasMorePages() {
		output, err = paginator.NextPage(context.TODO())
		if err == nil {
			b := make([]types.Instance, len(output.DBInstances))
			for i, v := range output.DBInstances {
				tags := types.Tags{}

				for _, v := range v.TagList {
					if v.Key != nil && v.Value != nil {
						tags[*v.Key] = *v.Value
					}
				}

				b[i] = types.Instance{
					AvailabilityZone:     v.AvailabilityZone,
					DBInstanceArn:        v.DBInstanceArn,
					DBInstanceIdentifier: v.DBInstanceIdentifier,
					DBInstanceClass:      v.DBInstanceClass,
					Engine:               v.Engine,
					EngineVersion:        v.EngineVersion,
					Tags:                 tags,
				}
			}
			dbInstances = append(dbInstances, b...)
		} else {
			return nil, err
		}
	}

	return dbInstances, nil
}
