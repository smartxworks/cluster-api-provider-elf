package manager

import "time"

const (
	defaultPrefix = "cape-"

	// DefaultSyncPeriod is the default value for the eponymous
	// manager option.
	DefaultSyncPeriod = time.Minute * 10

	// DefaultPodName is the default value for the eponymous manager option.
	DefaultPodName = defaultPrefix + "controller-manager"

	// DefaultPodNamespace is the default value for the eponymous manager option.
	DefaultPodNamespace = defaultPrefix + "system"

	// DefaultLeaderElectionID is the default value for the eponymous manager option.
	DefaultLeaderElectionID = DefaultPodName + "-runtime"
)
