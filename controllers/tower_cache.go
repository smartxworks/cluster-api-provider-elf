package controllers

import (
	"sync"
	"time"
)

const (
	silenceTime = time.Minute * 5
)

var clusterStatusMap = make(map[string]*clusterStatus)
var lock sync.Mutex

type clusterStatus struct {
	Resources resources
}

type resources struct {
	IsMemoryInsufficient bool
	LastUpdated          time.Time
	LastRtried           time.Time
}

// isElfClusterMemoryInsufficient returns whether the ELF cluster has insufficient memory,
// and whether to allow retry if insufficient memory.
func isElfClusterMemoryInsufficient(clusterID string) (bool, bool) {
	lock.Lock()
	defer lock.Unlock()

	if status, ok := clusterStatusMap[clusterID]; ok {
		if !status.Resources.IsMemoryInsufficient {
			return false, false
		}

		if time.Now().Add(-silenceTime).Before(status.Resources.LastUpdated) {
			return true, false
		} else {
			if time.Now().Add(-silenceTime).After(status.Resources.LastRtried) {
				status.Resources.LastRtried = time.Now()
				return true, true
			} else {
				return true, false
			}
		}
	}

	return false, false
}

// setElfClusterMemoryInsufficient sets whether the memory is insufficient.
func setElfClusterMemoryInsufficient(clusterID string, isInsufficient bool) {
	lock.Lock()
	defer lock.Unlock()

	resources := resources{
		IsMemoryInsufficient: isInsufficient,
		LastUpdated:          time.Now(),
	}

	if status, ok := clusterStatusMap[clusterID]; ok {
		if status.Resources.IsMemoryInsufficient != isInsufficient {
			status.Resources = resources
		}
	} else {
		clusterStatusMap[clusterID] = &clusterStatus{Resources: resources}
	}
}
