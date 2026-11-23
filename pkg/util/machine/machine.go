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

package machine

import (
	goctx "context"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	conditions "sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	labelsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
	typesutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/types"
)

const (
	// ProviderIDPrefix is the string data prefixed to a BIOS UUID in order
	// to build a provider ID.
	ProviderIDPrefix = "elf://"

	// ProviderIDPattern is a regex pattern and is used by ConvertProviderIDToUUID
	// to convert a providerID into a UUID string.
	ProviderIDPattern = `(?i)^` + ProviderIDPrefix + `([a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12})$`
)

// ErrNoMachineIPAddr indicates that no valid IP addresses were found in a machine context.
var ErrNoMachineIPAddr = errors.New("no IP addresses found for machine")

// GetElfMachinesInCluster gets a cluster's ElfMachine resources.
func GetElfMachinesInCluster(
	ctx goctx.Context,
	controllerClient client.Client,
	namespace, clusterName string) ([]*capev1.ElfMachine, error) {
	labels := map[string]string{capiv1.ClusterNameLabel: clusterName}
	var machineList capev1.ElfMachineList

	if err := controllerClient.List(
		ctx, &machineList,
		client.InNamespace(namespace),
		client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	machines := make([]*capev1.ElfMachine, len(machineList.Items))
	for i := range machineList.Items {
		machines[i] = &machineList.Items[i]
	}

	return machines, nil
}

// GetControlPlaneElfMachinesInCluster gets a cluster's Control Plane ElfMachine resources.
func GetControlPlaneElfMachinesInCluster(ctx goctx.Context, ctrlClient client.Client, namespace, clusterName string) ([]*capev1.ElfMachine, error) {
	var machineList capev1.ElfMachineList
	labels := map[string]string{
		capiv1.ClusterNameLabel:         clusterName,
		capiv1.MachineControlPlaneLabel: "",
	}

	if err := ctrlClient.List(ctx, &machineList, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	machines := make([]*capev1.ElfMachine, len(machineList.Items))
	for i := range machineList.Items {
		machines[i] = &machineList.Items[i]
	}

	return machines, nil
}

func GetElfMachinesForMD(
	ctx goctx.Context,
	ctrlClient client.Client,
	cluster *capiv1.Cluster,
	md *capiv1.MachineDeployment) ([]*capev1.ElfMachine, error) {
	elfMachineList := &capev1.ElfMachineList{}
	labels := map[string]string{
		capiv1.ClusterNameLabel:           cluster.Name,
		capiv1.MachineDeploymentNameLabel: md.Name,
	}
	if err := ctrlClient.List(ctx, elfMachineList, client.InNamespace(md.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	elfMachines := make([]*capev1.ElfMachine, len(elfMachineList.Items))
	for i := range elfMachineList.Items {
		elfMachines[i] = &elfMachineList.Items[i]
	}

	return elfMachines, nil
}

func GetControlPlaneMachinesForCluster(ctx goctx.Context, ctrlClient client.Client, cluster *capiv1.Cluster) ([]*capiv1.Machine, error) {
	ms := &capiv1.MachineList{}
	labels := map[string]string{
		capiv1.ClusterNameLabel:         cluster.Name,
		capiv1.MachineControlPlaneLabel: "",
	}

	if err := ctrlClient.List(ctx, ms, client.InNamespace(cluster.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	machines := make([]*capiv1.Machine, len(ms.Items))
	for i := range ms.Items {
		machines[i] = &ms.Items[i]
	}

	return machines, nil
}

// IsControlPlaneMachine returns true if the provided resource is
// a member of the control plane.
func IsControlPlaneMachine(machine metav1.Object) bool {
	labels := machine.GetLabels()
	if labels == nil {
		return false
	}

	_, ok := labels[capiv1.MachineControlPlaneLabel]
	return ok
}

// GetNodeGroupName returns the name of node group that the machine belongs.
func GetNodeGroupName(machine *capiv1.Machine) string {
	nodeGroupName := ""
	if IsControlPlaneMachine(machine) {
		nodeGroupName = GetKCPNameByMachine(machine)
	} else {
		nodeGroupName = labelsutil.GetDeploymentNameLabel(machine)
	}

	clusterName := labelsutil.GetClusterNameLabelLabel(machine)

	return strings.ReplaceAll(nodeGroupName, clusterName+"-", "")
}

// IsNodeHealthyConditionUnknown returns whether the node's healthy condition is unknown.
func IsNodeHealthyConditionUnknown(machine *capiv1.Machine) bool {
	if conditions.IsUnknown(machine, capiv1.MachineNodeHealthyCondition) &&
		conditions.GetReason(machine, capiv1.MachineNodeHealthyCondition) == capiv1.NodeConditionsFailedV1Beta1Reason {
		return true
	}

	return false
}

func IsMachineFailed(machine *capiv1.Machine) bool {
	return machine.Status.Deprecated != nil && machine.Status.Deprecated.V1Beta1 != nil &&
		(machine.Status.Deprecated.V1Beta1.FailureReason != nil || machine.Status.Deprecated.V1Beta1.FailureMessage != nil)
}

func ConvertProviderIDToUUID(providerID *string) string {
	if providerID == nil || *providerID == "" {
		return ""
	}

	pattern := regexp.MustCompile(ProviderIDPattern)
	matches := pattern.FindStringSubmatch(*providerID)
	if len(matches) < 2 {
		return ""
	}

	return matches[1]
}

func ConvertUUIDToProviderID(uuid string) string {
	if !typesutil.IsUUID(uuid) {
		return ""
	}

	return ProviderIDPrefix + uuid
}

func GetNetworkStatus(ipsStr string) []capev1.NetworkStatus {
	networks := []capev1.NetworkStatus{}

	if ipsStr == "" {
		return networks
	}

	ips := strings.Split(ipsStr, ",")
	for _, ip := range ips {
		if ip == "127.0.0.1" || strings.HasPrefix(ip, "169.254.") || strings.HasPrefix(ip, "172.17.0") {
			continue
		}

		networks = append(networks, capev1.NetworkStatus{
			IPAddrs: []string{ip},
		})
	}

	return networks
}
