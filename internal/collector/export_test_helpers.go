package collector

import "time"

// GetQueryTimeout returns the queryTimeout field for testing.
func GetQueryTimeout(c *Collector) time.Duration {
	return c.queryTimeout
}

// GetTargetTimeout returns the targetTimeout field for testing.
func GetTargetTimeout(c *Collector) time.Duration {
	return c.targetTimeout
}
