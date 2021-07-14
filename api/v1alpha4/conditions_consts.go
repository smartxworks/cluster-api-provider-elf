package v1alpha4

import clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"

// Conditions and condition Reasons for the ElfMachine object.

const (
	// VMProvisionedCondition documents the status of the provisioning of a VM.
	VMProvisionedCondition clusterv1.ConditionType = "VMProvisioned"

	// WaitingForClusterInfrastructureReason (Severity=Info) documents a ElfMachine waiting for the cluster
	// infrastructure to be ready before starting the provisioning process.
	WaitingForClusterInfrastructureReason = "WaitingForClusterInfrastructure"

	// WaitingForBootstrapDataReason (Severity=Info) documents a ElfMachine waiting for the bootstrap
	// script to be ready before starting the provisioning process.
	WaitingForBootstrapDataReason = "WaitingForBootstrapData"

	// CloningReason documents (Severity=Info) ElfMachine currently executing the clone operation.
	CloningReason = "Cloning"

	// CloningFailedReason (Severity=Warning) documents a ElfMachine controller detecting
	// an error while provisioning; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	CloningFailedReason = "CloningFailed"

	// TaskFailure (Severity=Warning) documents a ElfMachine task failure; the reconcile look will automatically
	// retry the operation, but a user intervention might be required to fix the problem.
	TaskFailure = "TaskFailure"

	// WaitingForNetworkAddressesReason (Severity=Info) documents a ElfMachine waiting for the the machine network
	// settings to be reported after machine being powered on.
	WaitingForNetworkAddressesReason = "WaitingForNetworkAddresses"
)

// Conditions and Reasons related to make connections to a ELF. Can currently be used by ElfCluster and ElfMachine

const (
	// ELfAvailableCondition documents the connectivity with elf
	ElfAvailableCondition clusterv1.ConditionType = "ElfAvailable"

	// ElfUnreachableReason (Severity=Error) documents a controller detecting
	// issues with Elf reachability;
	ElfUnreachableReason = "ElfUnreachable"
)
