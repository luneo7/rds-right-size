package types

type Instance struct {
	// The Availability Zone that the automated backup was created in. For information
	// on Amazon Web Services Regions and Availability Zones, see Regions and
	// Availability Zones (https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Concepts.RegionsAndAvailabilityZones.html)
	// .
	AvailabilityZone *string

	// The Amazon Resource Name (ARN)
	DBInstanceArn *string

	// The identifier for the source DB instance, which can't be changed and which is
	// unique to an Amazon Web Services Region.
	DBInstanceIdentifier *string

	// The name of the compute and memory capacity class of the DB instance.
	DBInstanceClass *string

	// The name of the database engine for this automated backup.
	Engine *string

	// The version of the database engine for the automated backup.
	EngineVersion *string

	Tags Tags
}

type Tags map[string]string
