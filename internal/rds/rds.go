package rds

import (
	"context"
	"strconv"

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

				var paramGroupName *string
				if len(v.DBParameterGroups) > 0 && v.DBParameterGroups[0].DBParameterGroupName != nil {
					paramGroupName = v.DBParameterGroups[0].DBParameterGroupName
				}

				b[i] = types.Instance{
					AvailabilityZone:     v.AvailabilityZone,
					DBInstanceArn:        v.DBInstanceArn,
					DBInstanceIdentifier: v.DBInstanceIdentifier,
					DBInstanceClass:      v.DBInstanceClass,
					Engine:               v.Engine,
					EngineVersion:        v.EngineVersion,
					DBParameterGroupName: paramGroupName,
					DBClusterIdentifier:  v.DBClusterIdentifier,
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

// GetMaxConnections queries the DB parameter group for the max_connections setting.
// Returns the numeric value if explicitly set to a static number, or nil if it's
// a formula, unset, or if the API call fails. The caller should fall back to the
// JSON-defined maxConnections for the target instance type when nil is returned.
func (r *RDS) GetMaxConnections(paramGroupName *string) (*int64, error) {
	if paramGroupName == nil || *paramGroupName == "" {
		return nil, nil
	}

	paginator := awsRds.NewDescribeDBParametersPaginator(r.rdsClient, &awsRds.DescribeDBParametersInput{
		DBParameterGroupName: paramGroupName,
	})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.TODO())
		if err != nil {
			return nil, err
		}

		for _, param := range output.Parameters {
			if param.ParameterName != nil && *param.ParameterName == "max_connections" {
				if param.ParameterValue == nil || *param.ParameterValue == "" {
					return nil, nil
				}

				// Try to parse as integer. If it's a formula like
				// "{DBInstanceClassMemory/12582880}" or "LEAST(...)", this will fail
				// and we return nil to signal fallback to JSON defaults.
				val, err := strconv.ParseInt(*param.ParameterValue, 10, 64)
				if err != nil {
					return nil, nil
				}

				return &val, nil
			}
		}
	}

	return nil, nil
}
