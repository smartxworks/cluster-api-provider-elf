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

	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

const (
	resourceSilenceTime = 5 * time.Minute
	// resourceDuration is the lifespan of clusterResource.
	resourceDuration = 10 * time.Minute
)

var lock sync.Mutex

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
// 2. ELF cluster has insufficient storage.
// 3. Placement group not satisfy policy.
func isELFScheduleVMErrorRecorded(ctx *context.MachineContext) (bool, string, error) {
	lock.Lock()
	defer lock.Unlock()

	if resource := getClusterResource(getKeyForInsufficientMemoryError(ctx.ElfCluster.Spec.Cluster)); resource != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForELFClusterWithSufficientMemoryReason, clusterv1.ConditionSeverityInfo, "")

		return true, fmt.Sprintf("Insufficient memory detected for the ELF cluster %s", ctx.ElfCluster.Spec.Cluster), nil
	} else if resource := getClusterResource(getKeyForInsufficientStorageError(ctx.ElfCluster.Spec.Cluster)); resource != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForELFClusterWithSufficientStorageReason, clusterv1.ConditionSeverityInfo, "")

		return true, fmt.Sprintf("Insufficient storage detected for the ELF cluster %s", ctx.ElfCluster.Spec.Cluster), nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return false, "", err
	}

	if resource := getClusterResource(getKeyForDuplicatePlacementGroupError(placementGroupName)); resource != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForPlacementGroupPolicySatisfiedReason, clusterv1.ConditionSeverityInfo, "")

		return true, fmt.Sprintf("Not satisfy policy detected for the placement group %s", placementGroupName), nil
	}

	return false, "", nil
}

// recordElfClusterMemoryInsufficient records whether the memory is insufficient.
func recordElfClusterMemoryInsufficient(ctx *context.MachineContext, isInsufficient bool) {
	key := getKeyForInsufficientMemoryError(ctx.ElfCluster.Spec.Cluster)
	if isInsufficient {
		inMemoryCache.Set(key, newClusterResource(), resourceDuration)
	} else {
		inMemoryCache.Delete(key)
	}
}

// recordElfClusterStorageInsufficient records whether the storage is insufficient.
func recordElfClusterStorageInsufficient(ctx *context.MachineContext, isError bool) {
	key := getKeyForInsufficientStorageError(ctx.ElfCluster.Spec.Cluster)
	if isError {
		inMemoryCache.Set(key, newClusterResource(), resourceDuration)
	} else {
		inMemoryCache.Delete(key)
	}
}

// recordPlacementGroupPolicyNotSatisfied records whether the placement group not satisfy policy.
func recordPlacementGroupPolicyNotSatisfied(ctx *context.MachineContext, isPGPolicyNotSatisfied bool) error {
	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return err
	}

	key := getKeyForDuplicatePlacementGroupError(placementGroupName)
	if isPGPolicyNotSatisfied {
		inMemoryCache.Set(key, newClusterResource(), resourceDuration)
	} else {
		inMemoryCache.Delete(key)
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

	if ok := canRetry(getKeyForInsufficientStorageError(ctx.ElfCluster.Spec.Cluster)); ok {
		return true, nil
	} else if ok := canRetry(getKeyForInsufficientMemoryError(ctx.ElfCluster.Spec.Cluster)); ok {
		return true, nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return false, err
	}

	return canRetry(getKeyForDuplicatePlacementGroupError(placementGroupName)), nil
}

func canRetry(key string) bool {
	if resource := getClusterResource(key); resource != nil {
		if time.Now().Before(resource.LastDetected.Add(resourceSilenceTime)) {
			return false
		} else {
			if time.Now().Before(resource.LastRetried.Add(resourceSilenceTime)) {
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
	if val, found := inMemoryCache.Get(key); found {
		if resource, ok := val.(*clusterResource); ok {
			return resource
		}

		// Delete unexpected data.
		inMemoryCache.Delete(key)
	}

	return nil
}

func getKeyForInsufficientStorageError(clusterID string) string {
	return fmt.Sprintf("insufficient:storage:%s", clusterID)
}

func getKeyForInsufficientMemoryError(clusterID string) string {
	return fmt.Sprintf("insufficient:memory:%s", clusterID)
}

func getKeyForDuplicatePlacementGroupError(placementGroup string) string {
	return fmt.Sprintf("pg:duplicate:%s", placementGroup)
}

// pgCacheDuration is the lifespan of placement group cache.
const pgCacheDuration = 20 * time.Second

func getKeyForPGCache(pgName string) string {
	return fmt.Sprintf("pg:%s:cache", pgName)
}

// setPGCache saves the specified placement group to the memory,
// which can reduce access to the Tower service.
func setPGCache(pg *models.VMPlacementGroup) {
	inMemoryCache.Set(getKeyForPGCache(*pg.Name), *pg, pgCacheDuration)
}

// delPGCaches deletes the specified placement group caches.
func delPGCaches(pgNames []string) {
	for i := 0; i < len(pgNames); i++ {
		inMemoryCache.Delete(getKeyForPGCache(pgNames[i]))
	}
}

// getPGFromCache gets the specified placement group from the memory.
func getPGFromCache(pgName string) *models.VMPlacementGroup {
	key := getKeyForPGCache(pgName)
	if val, found := inMemoryCache.Get(key); found {
		if pg, ok := val.(models.VMPlacementGroup); ok {
			return &pg
		}
		// Delete unexpected data.
		inMemoryCache.Delete(key)
	}

	return nil
}

// labelCacheDuration is the lifespan of label cache.
const labelCacheDuration = 10 * time.Minute

func getKeyForLabelCache(labelKey string) string {
	return fmt.Sprintf("label:%s:cache", labelKey)
}

// setLabelInCache saves the specified label to the memory,
// which can reduce access to the Tower service.
func setLabelInCache(label *models.Label) {
	inMemoryCache.Set(getKeyForLabelCache(*label.Key), *label, labelCacheDuration)
}

// delLabelCache deletes the specified label cache.
func delLabelCache(labelKey string) {
	inMemoryCache.Delete(getKeyForLabelCache(labelKey))
}

// getLabelFromCache gets the specified label from the memory.
func getLabelFromCache(labelKey string) *models.Label {
	key := getKeyForLabelCache(labelKey)
	if val, found := inMemoryCache.Get(key); found {
		if label, ok := val.(models.Label); ok {
			return &label
		}
		// Delete unexpected data.
		inMemoryCache.Delete(key)
	}

	return nil
}

/* GPU */

// gpuCacheDuration is the lifespan of gpu cache.
const gpuCacheDuration = 3 * time.Second

func getKeyForGPUVMInfo(gpuID string) string {
	return fmt.Sprintf("gpu:vm:info:%s", gpuID)
}

// setGPUVMInfosCache saves the specified GPU device infos to the memory,
// which can reduce access to the Tower service.
func setGPUVMInfosCache(gpuVMInfos service.GPUVMInfos) {
	gpuVMInfos.Iterate(func(g *models.GpuVMInfo) {
		inMemoryCache.Set(getKeyForGPUVMInfo(*g.ID), *g, gpuCacheDuration)
	})
}

// setGPUDeviceInfosCache gets the specified GPU device infos from the memory.
func getGPUVMInfosFromCache(gpuIDs []string) service.GPUVMInfos {
	gpuVMInfos := service.NewGPUVMInfos()
	for i := 0; i < len(gpuIDs); i++ {
		key := getKeyForGPUVMInfo(gpuIDs[i])
		if val, found := inMemoryCache.Get(key); found {
			if gpuVMInfo, ok := val.(models.GpuVMInfo); ok {
				gpuVMInfos.Insert(&gpuVMInfo)
			}
			// Delete unexpected data.
			inMemoryCache.Delete(key)
		}
	}

	return gpuVMInfos
}
