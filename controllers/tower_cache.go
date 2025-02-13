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
	goctx "context"
	"fmt"
	"sync"
	"time"

	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/apimachinery/pkg/api/resource"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

const (
	resourceSilenceTime = 5 * time.Minute
	// resourceDuration is the lifespan of clusterResource.
	resourceDuration = 10 * time.Minute

	// defaultStorageAmount is the default storage amount. now we use 200GB.
	defaultStorageAmount = 200
)

var lock sync.Mutex

type clusterResource struct {
	// LastDetected records the last resource detection time
	LastDetected time.Time
	// LastRetried records the time of the last attempt to detect resource
	LastRetried time.Time
	// RequestAmount records the amount of resources requested when cluster resource insufficiency is detected
	RequestAmount int64
}

// isELFScheduleVMErrorRecorded returns whether the ELF cluster has failed scheduling virtual machine errors.
//
// Includes these scenarios:
// 1. ELF cluster has insufficient memory.
// 2. ELF cluster has insufficient storage.
// 3. Cannot satisfy the PlacementGroup policy.
func isELFScheduleVMErrorRecorded(ctx goctx.Context, machineCtx *context.MachineContext, ctrlClient client.Client) (bool, string, error) {
	lock.Lock()
	defer lock.Unlock()

	if insufficient, msg := isELFClusterMemoryInsufficient(machineCtx); insufficient {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForELFClusterWithSufficientMemoryReason, clusterv1.ConditionSeverityInfo, "")
		return true, msg, nil
	} else if insufficient, msg := isELFClusterStorageInsufficient(machineCtx); insufficient {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForELFClusterWithSufficientStorageReason, clusterv1.ConditionSeverityInfo, "")
		return true, msg, nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlClient, machineCtx.Machine, machineCtx.Cluster)
	if err != nil {
		return false, "", err
	}

	if resource := getClusterResource(getKeyForDuplicatePlacementGroupError(placementGroupName)); resource != nil {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForPlacementGroupPolicySatisfiedReason, clusterv1.ConditionSeverityInfo, "")

		return true, "Not satisfy policy detected for the placement group " + placementGroupName, nil
	}

	return false, "", nil
}

func isELFClusterMemoryInsufficient(machineCtx *context.MachineContext) (bool, string) {
	if resource := getClusterResource(getKeyForInsufficientMemoryError(machineCtx.ElfCluster.Spec.Cluster)); resource != nil {
		return true, "Insufficient memory detected for the ELF cluster " + machineCtx.ElfCluster.Spec.Cluster
	}
	return false, ""
}

func isELFClusterStorageInsufficient(machineCtx *context.MachineContext) (bool, string) {
	if resource := getClusterResource(getKeyForInsufficientStorageError(machineCtx.ElfCluster.Spec.Cluster)); resource != nil {
		return true, "Insufficient storage detected for the ELF cluster " + machineCtx.ElfCluster.Spec.Cluster
	}
	return false, ""
}

// recordElfClusterMemoryInsufficient records whether the memory is insufficient.
func recordElfClusterMemoryInsufficient(machineCtx *context.MachineContext, isInsufficient bool) {
	key := getKeyForInsufficientMemoryError(machineCtx.ElfCluster.Spec.Cluster)
	if isInsufficient {
		inMemoryCache.Set(key, newClusterResource(getMemoryRequestAmount(machineCtx)), resourceDuration)
	} else {
		inMemoryCache.Delete(key)
	}
}

// recordElfClusterStorageInsufficient records whether the storage is insufficient.
func recordElfClusterStorageInsufficient(machineCtx *context.MachineContext, isError bool) {
	key := getKeyForInsufficientStorageError(machineCtx.ElfCluster.Spec.Cluster)
	if isError {
		inMemoryCache.Set(key, newClusterResource(getStorageRequestAmount(machineCtx)), resourceDuration)
	} else {
		inMemoryCache.Delete(key)
	}
}

// recordPlacementGroupPolicyNotSatisfied records whether the placement group not satisfy policy.
func recordPlacementGroupPolicyNotSatisfied(ctx goctx.Context, machineCtx *context.MachineContext, ctrlClient client.Client, isPGPolicyNotSatisfied bool) error {
	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlClient, machineCtx.Machine, machineCtx.Cluster)
	if err != nil {
		return err
	}

	key := getKeyForDuplicatePlacementGroupError(placementGroupName)
	if isPGPolicyNotSatisfied {
		inMemoryCache.Set(key, newClusterResource(0), resourceDuration)
	} else {
		inMemoryCache.Delete(key)
	}

	return nil
}

func newClusterResource(requestAmount int64) *clusterResource {
	now := time.Now()
	return &clusterResource{
		LastDetected:  now,
		LastRetried:   now,
		RequestAmount: requestAmount,
	}
}

func getMemoryRequestAmount(machineCtx *context.MachineContext) int64 {
	specMemory := *resource.NewQuantity(machineCtx.ElfMachine.Spec.MemoryMiB*1024*1024, resource.BinarySI)
	return specMemory.Value() - machineCtx.ElfMachine.Status.Resources.Memory.Value()
}

func getStorageRequestAmount(machineCtx *context.MachineContext) int64 {
	if specDisk := machineCtx.ElfMachine.Spec.DiskGiB; specDisk == 0 {
		return defaultStorageAmount
	}
	return int64(machineCtx.ElfMachine.Spec.DiskGiB - machineCtx.ElfMachine.Status.Resources.Disk)
}

// canRetryVMOperation returns whether virtual machine operations(Create/PowerOn)
// can be performed.
// For insufficient memory and storage resources, if the requested amount
// of resources is less than the recorded request amount, retry is allowed.
func canRetryVMOperation(ctx goctx.Context, machineCtx *context.MachineContext, ctrlClient client.Client) (bool, error) {
	lock.Lock()
	defer lock.Unlock()

	if ok := canRetryMemoryAllocation(machineCtx); ok {
		return true, nil
	} else if ok := canRetryStorageAllocation(machineCtx); ok {
		return true, nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlClient, machineCtx.Machine, machineCtx.Cluster)
	if err != nil {
		return false, err
	}

	return canRetry(getKeyForDuplicatePlacementGroupError(placementGroupName), 0), nil
}

func canRetryMemoryAllocation(machineCtx *context.MachineContext) bool {
	return canRetry(getKeyForInsufficientMemoryError(machineCtx.ElfCluster.Spec.Cluster), getMemoryRequestAmount(machineCtx))
}

func canRetryStorageAllocation(machineCtx *context.MachineContext) bool {
	return canRetry(getKeyForInsufficientStorageError(machineCtx.ElfCluster.Spec.Cluster), getStorageRequestAmount(machineCtx))
}

func canRetry(key string, requestAmount int64) bool {
	if resource := getClusterResource(key); resource != nil {
		if requestAmount < resource.RequestAmount {
			return true
		}
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
	return "insufficient:storage:" + clusterID
}

func getKeyForInsufficientMemoryError(clusterID string) string {
	return "insufficient:memory:" + clusterID
}

func getKeyForDuplicatePlacementGroupError(placementGroup string) string {
	return "pg:duplicate:" + placementGroup
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
	for i := range len(pgNames) {
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
	return "gpu:vm:info:" + gpuID
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
	for i := range len(gpuIDs) {
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
