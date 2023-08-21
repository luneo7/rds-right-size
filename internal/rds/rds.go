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
				b[i] = types.Instance{
					AvailabilityZone:     v.AvailabilityZone,
					DBInstanceArn:        v.DBInstanceArn,
					DBInstanceIdentifier: v.DBInstanceIdentifier,
					DBInstanceClass:      v.DBInstanceClass,
					Engine:               v.Engine,
					EngineVersion:        v.EngineVersion,
				}
			}
			dbInstances = append(dbInstances, b...)
		} else {
			return nil, err
		}
	}

	return dbInstances, nil
}

func (r *RDS) GetTags(dbInstanceArn *string) (types.Tags, error) {
	tags := types.Tags{}

	output, err := r.rdsClient.ListTagsForResource(
		context.Background(),
		&awsRds.ListTagsForResourceInput{
			ResourceName: dbInstanceArn,
		},
	)

	if err != nil {
		return tags, err
	}

	for _, v := range output.TagList {
		if v.Key != nil && v.Value != nil {
			tags[*v.Key] = *v.Value
		}
	}

	return tags, nil
}
