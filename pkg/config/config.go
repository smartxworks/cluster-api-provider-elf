package config

import "time"

var (
	// DefaultRequeue is the default time for how long to wait when
	// requeueing a CAPE operation.
	DefaultRequeue = 20 * time.Second
)
