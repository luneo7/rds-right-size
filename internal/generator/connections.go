package generator

import "strings"

// AuroraMySQLMaxConnections returns the default max_connections for Aurora MySQL
// based on the instance size suffix. These values come from the AWS documentation
// lookup table, as Aurora MySQL uses a log-based formula that doesn't simplify
// to a clean expression.
func AuroraMySQLMaxConnections(dbInstanceClass string) *int64 {
	// Extract size suffix (e.g., "xlarge" from "db.r6g.xlarge")
	parts := strings.Split(dbInstanceClass, ".")
	if len(parts) < 3 {
		return nil
	}

	family := parts[1] // e.g., "r6g", "t3"
	size := parts[2]   // e.g., "xlarge", "medium"

	isT := strings.HasPrefix(family, "t")

	if isT {
		return auroraMyTClass(size)
	}
	return auroraMyRClass(size)
}

func auroraMyTClass(size string) *int64 {
	table := map[string]int64{
		"micro":  45,
		"small":  45,
		"medium": 90,
		"large":  270,
	}
	if v, ok := table[size]; ok {
		return &v
	}
	return nil
}

func auroraMyRClass(size string) *int64 {
	table := map[string]int64{
		"large":    1000,
		"xlarge":   2000,
		"2xlarge":  3000,
		"4xlarge":  4000,
		"8xlarge":  5000,
		"12xlarge": 6000,
		"16xlarge": 6000,
		"24xlarge": 7000,
		"32xlarge": 7000,
		"48xlarge": 8000,
	}
	if v, ok := table[size]; ok {
		return &v
	}
	return nil
}

// AuroraPostgreSQLMaxConnections computes the default max_connections for Aurora PostgreSQL
// using the documented formula: LEAST(DBInstanceClassMemory / 9531392, 5000)
// We approximate DBInstanceClassMemory as raw memory * 0.9 (accounting for OS/RDS overhead).
func AuroraPostgreSQLMaxConnections(memoryGiB int64) *int64 {
	if memoryGiB <= 0 {
		return nil
	}

	// Convert GiB to bytes, apply 0.9 factor for DBInstanceClassMemory approximation
	memoryBytes := float64(memoryGiB) * 1024 * 1024 * 1024 * 0.9

	maxConns := int64(memoryBytes / 9531392)
	if maxConns > 5000 {
		maxConns = 5000
	}

	return &maxConns
}

// GetMaxConnections returns the appropriate max_connections value based on engine type.
func GetMaxConnections(engine string, dbInstanceClass string, memoryGiB int64) *int64 {
	switch engine {
	case "aurora-mysql":
		return AuroraMySQLMaxConnections(dbInstanceClass)
	case "aurora-postgresql":
		return AuroraPostgreSQLMaxConnections(memoryGiB)
	default:
		return AuroraMySQLMaxConnections(dbInstanceClass)
	}
}
