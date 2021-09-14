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

	// PoweringOnReason documents (Severity=Info) a ElfMachine currently executing the power on sequence.
	PoweringOnReason = "PoweringOn"

	// PoweringOnFailedReason (Severity=Warning) documents a ElfMachine controller detecting
	// an error while powering on; those kind of errors are usually transient and failed provisioning
	// are automatically re-tried by the controller.
	PoweringOnFailedReason = "PoweringOnFailed"

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

// Conditions and Reasons related to make connections to a Tower. Can currently be used by ElfCluster and ElfMachine

const (
	// TowerAvailableCondition documents the connectivity with tower
	TowerAvailableCondition clusterv1.ConditionType = "TowerAvailable"

	// TowerUnreachableReason (Severity=Error) documents a controller detecting
	// issues with tower reachability;
	TowerUnreachableReason = "TowerUnreachable"
)
