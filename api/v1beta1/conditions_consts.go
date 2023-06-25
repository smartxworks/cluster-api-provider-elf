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

package v1beta1

import clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

// Conditions and condition Reasons for the ElfMachine object.

const (
	// VMProvisionedCondition documents the status of the provisioning of a VM.
	VMProvisionedCondition clusterv1.ConditionType = "VMProvisioned"

	// WaitingForClusterInfrastructureReason (Severity=Info) documents an ElfMachine waiting for the cluster
	// infrastructure to be ready before starting the provisioning process.
	WaitingForClusterInfrastructureReason = "WaitingForClusterInfrastructure"

	// WaitingForBootstrapDataReason (Severity=Info) documents an ElfMachine waiting for the bootstrap
	// script to be ready before starting the provisioning process.
	WaitingForBootstrapDataReason = "WaitingForBootstrapData"

	// WaitingForStaticIPAllocationReason (Severity=Info) documents an ElfMachine waiting for the allocation of
	// a static IP address.
	WaitingForStaticIPAllocationReason = "WaitingForStaticIPAllocation"

	// CloningReason documents (Severity=Info) ElfMachine currently executing the clone operation.
	CloningReason = "Cloning"

	// UpdatingReason documents (Severity=Info) ElfMachine currently executing the update operation.
	UpdatingReason = "Updating"

	// PoweringOnReason documents (Severity=Info) an ElfMachine currently executing the power on sequence.
	PoweringOnReason = "PoweringOn"

	// PowerOffReason documents (Severity=Info) an ElfMachine currently executing the power off sequence.
	PowerOffReason = "PoweringOff"

	// ShuttingDownReason documents (Severity=Info) an ElfMachine currently executing the shut down sequence.
	ShuttingDownReason = "ShuttingDown"

	// PoweringOnFailedReason (Severity=Warning) documents an ElfMachine controller detecting
	// an error while powering on; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	PoweringOnFailedReason = "PoweringOnFailed"

	// PoweringOffFailedReason (Severity=Warning) documents an ElfMachine controller detecting
	// an error while powering off; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	PoweringOffFailedReason = "PoweringOffFailed"

	// ShuttingDownFailedReason (Severity=Warning) documents an ElfMachine controller detecting
	// an error while shutting down; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	ShuttingDownFailedReason = "ShuttingDownFailed"

	// CloningFailedReason (Severity=Warning) documents an ElfMachine controller detecting
	// an error while provisioning; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	CloningFailedReason = "CloningFailed"

	// UpdatingFailedReason (Severity=Warning) documents an ElfMachine controller detecting
	// an error while updating; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	UpdatingFailedReason = "UpdatingFailed"

	// TaskFailureReason (Severity=Warning) documents an ElfMachine task failure; the reconcile look will automatically
	// retry the operation, but a user intervention might be required to fix the problem.
	TaskFailureReason = "TaskFailure"

	// WaitingForNetworkAddressesReason (Severity=Info) documents an ElfMachine waiting for the machine network
	// settings to be reported after machine being powered on.
	WaitingForNetworkAddressesReason = "WaitingForNetworkAddresses"

	// JoiningPlacementGroupReason documents (Severity=Info) an ElfMachine currently executing the join placement group operation.
	JoiningPlacementGroupReason = "JoiningPlacementGroup"

	// JoiningPlacementGroupFailedReason (Severity=Warning) documents an ElfMachine controller detecting
	// an error while joining placement group; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	JoiningPlacementGroupFailedReason = "JoiningPlacementGroupFailed"

	// WaitingForAvailableHostRequiredByPlacementGroupReason (Severity=Info) documents an ElfMachine
	// waiting for an available host required by placement group to create VM.
	WaitingForAvailableHostRequiredByPlacementGroupReason = "WaitingForAvailableHostRequiredByPlacementGroup"
)

// Conditions and Reasons related to make connections to a Tower. Can currently be used by ElfCluster and ElfMachine

const (
	// TowerAvailableCondition documents the connectivity with tower.
	TowerAvailableCondition clusterv1.ConditionType = "TowerAvailable"

	// TowerUnreachableReason (Severity=Error) documents a controller detecting
	// issues with tower reachability.
	TowerUnreachableReason = "TowerUnreachable"
)

// Conditions and condition Reasons for the ElfCluster object.

const (
	// ControlPlaneEndpointReadyCondition documents the status of control plane endpoint.
	ControlPlaneEndpointReadyCondition clusterv1.ConditionType = "ControlPlaneEndpointReady"

	// WaitingForVIPReason (Severity=Info) documents the control plane endpoint of ElfCluster
	// waiting for an IP Address and port.
	WaitingForVIPReason = "WaitingForVIP"
)
