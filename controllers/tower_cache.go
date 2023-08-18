/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"fmt"
	"sync"
	"time"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
)

const (
	silenceTime = time.Minute * 5
	// resourceDuration is the lifespan of clusterResource.
	resourceDuration   = 10 * time.Minute
	resourceGCInterval = 10 * time.Minute
)

var clusterResourceMap = newTTLMap(resourceGCInterval)
var lock sync.RWMutex

type clusterResource struct {
	// LastDetected records the last resource detection time
	LastDetected time.Time
	// LastRetried records the time of the last attempt to detect resource
	LastRetried time.Time
}

// isELFScheduleVMErrorRecorded returns whether the ELF cluster has failed scheduling virtual machine errors.
//
// Includes these scenarios:
// 1. ELF cluster has insufficient memory.
// 2. Placement group not satisfy policy.
func isELFScheduleVMErrorRecorded(ctx *context.MachineContext) (bool, string, error) {
	lock.RLock()
	defer lock.RUnlock()

	if resource := getClusterResource(getMemoryKey(ctx.ElfCluster.Spec.Cluster)); resource != nil {
		return true, fmt.Sprintf("Insufficient memory detected for the ELF cluster %s", ctx.ElfCluster.Spec.Cluster), nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return false, "", err
	}

	if resource := getClusterResource(getPlacementGroupKey(placementGroupName)); resource != nil {
		return true, fmt.Sprintf("Not satisfy policy detected for the placement group %s", placementGroupName), nil
	}

	return false, "", nil
}

// recordElfClusterMemoryInsufficient records whether the memory is insufficient.
func recordElfClusterMemoryInsufficient(ctx *context.MachineContext, isInsufficient bool) {
	lock.Lock()
	defer lock.Unlock()

	key := getMemoryKey(ctx.ElfCluster.Spec.Cluster)
	if isInsufficient {
		clusterResourceMap.Set(getMemoryKey(ctx.ElfCluster.Spec.Cluster), newClusterResource(), resourceDuration)
	} else {
		clusterResourceMap.Del(key)
	}
}

// recordPlacementGroupPolicyNotSatisfied records whether the placement group not satisfy policy.
func recordPlacementGroupPolicyNotSatisfied(ctx *context.MachineContext, isNotSatisfiedPolicy bool) error {
	lock.Lock()
	defer lock.Unlock()

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return err
	}

	key := getPlacementGroupKey(placementGroupName)
	if isNotSatisfiedPolicy {
		clusterResourceMap.Set(key, newClusterResource(), resourceDuration)
	} else {
		clusterResourceMap.Del(key)
	}

	return nil
}

func newClusterResource() *clusterResource {
	now := time.Now()
	return &clusterResource{
		LastDetected: now,
		LastRetried:  now,
	}
}

// canRetryVMOperation returns whether virtual machine operations(Create/PowerOn)
// can be performed.
func canRetryVMOperation(ctx *context.MachineContext) (bool, error) {
	lock.Lock()
	defer lock.Unlock()

	if ok := canRetry(getMemoryKey(ctx.ElfCluster.Spec.Cluster)); ok {
		return true, nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return false, err
	}

	return canRetry(getPlacementGroupKey(placementGroupName)), nil
}

func canRetry(key string) bool {
	if resource := getClusterResource(key); resource != nil {
		if time.Now().Before(resource.LastDetected.Add(silenceTime)) {
			return false
		} else {
			if time.Now().Before(resource.LastRetried.Add(silenceTime)) {
				return false
			} else {
				resource.LastRetried = time.Now()
				return true
			}
		}
	}

	return false
}

func getClusterResource(key string) *clusterResource {
	if val, ok := clusterResourceMap.Get(key); ok {
		if resource, ok := val.(*clusterResource); ok {
			return resource
		}

		// Delete unexpected data.
		clusterResourceMap.Del(key)
	}

	return nil
}

func getMemoryKey(clusterID string) string {
	return fmt.Sprintf("%s-insufficient-memory", clusterID)
}

func getPlacementGroupKey(placementGroup string) string {
	return fmt.Sprintf("%s-duplicate-placement-group", placementGroup)
}
