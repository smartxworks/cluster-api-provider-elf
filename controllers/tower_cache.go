package controllers

import (
	"sync"
	"time"
)

const (
	silenceTime = time.Minute * 5
)

var clusterStatusMap = make(map[string]*clusterStatus)
var lock sync.RWMutex

type clusterStatus struct {
	Resources resources
}

type resources struct {
	IsMemoryInsufficient bool
	// LastDetected records the last memory detection time
	LastDetected time.Time
	// LastRetried records the time of the last attempt to detect memory
	LastRetried time.Time
}

// isElfClusterMemoryInsufficient returns whether the ELF cluster has insufficient memory.
func isElfClusterMemoryInsufficient(clusterID string) bool {
	lock.RLock()
	defer lock.RUnlock()

	if status, ok := clusterStatusMap[clusterID]; ok {
		return status.Resources.IsMemoryInsufficient
	}

	return false
}

// setElfClusterMemoryInsufficient sets whether the memory is insufficient.
func setElfClusterMemoryInsufficient(clusterID string, isInsufficient bool) {
	lock.Lock()
	defer lock.Unlock()

	now := time.Now()
	resources := resources{
		IsMemoryInsufficient: isInsufficient,
		LastDetected:         now,
		LastRetried:          now,
	}

	if status, ok := clusterStatusMap[clusterID]; ok {
		status.Resources = resources
	} else {
		clusterStatusMap[clusterID] = &clusterStatus{Resources: resources}
	}
}

// needDetectElfClusterMemoryInsufficient returns whether need to detect if insufficient memory.
func needDetectElfClusterMemoryInsufficient(clusterID string) bool {
	lock.Lock()
	defer lock.Unlock()

	if status, ok := clusterStatusMap[clusterID]; ok {
		if !status.Resources.IsMemoryInsufficient {
			return false
		}

		if time.Now().Before(status.Resources.LastDetected.Add(silenceTime)) {
			return false
		} else {
			if time.Now().Before(status.Resources.LastRetried.Add(silenceTime)) {
				return false
			} else {
				status.Resources.LastRetried = time.Now()
				return true
			}
		}
	}

	return false
}
