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
	"fmt"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	labelsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
)

const (
	// ProviderIDPrefix is the string data prefixed to a BIOS UUID in order
	// to build a provider ID.
	ProviderIDPrefix = "elf://"

	// ProviderIDPattern is a regex pattern and is used by ConvertProviderIDToUUID
	// to convert a providerID into a UUID string.
	ProviderIDPattern = `(?i)^` + ProviderIDPrefix + `([a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12})$`

	// UUIDPattern is a regex pattern and is used by ConvertUUIDToProviderID
	// to convert a UUID into a providerID string.
	UUIDPattern = `(?i)^[a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12}$`
)

// ErrNoMachineIPAddr indicates that no valid IP addresses were found in a machine context.
var ErrNoMachineIPAddr = errors.New("no IP addresses found for machine")

// GetElfMachinesInCluster gets a cluster's ElfMachine resources.
func GetElfMachinesInCluster(
	ctx goctx.Context,
	controllerClient client.Client,
	namespace, clusterName string) ([]*infrav1.ElfMachine, error) {
	labels := map[string]string{clusterv1.ClusterNameLabel: clusterName}
	var machineList infrav1.ElfMachineList

	if err := controllerClient.List(
		ctx, &machineList,
		client.InNamespace(namespace),
		client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	machines := make([]*infrav1.ElfMachine, len(machineList.Items))
	for i := range machineList.Items {
		machines[i] = &machineList.Items[i]
	}

	return machines, nil
}

// GetControlPlaneElfMachinesInCluster gets a cluster's Control Plane ElfMachine resources.
func GetControlPlaneElfMachinesInCluster(ctx goctx.Context, ctrlClient client.Client, namespace, clusterName string) ([]*infrav1.ElfMachine, error) {
	var machineList infrav1.ElfMachineList
	labels := map[string]string{
		clusterv1.ClusterNameLabel:         clusterName,
		clusterv1.MachineControlPlaneLabel: "",
	}

	if err := ctrlClient.List(ctx, &machineList, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	machines := make([]*infrav1.ElfMachine, len(machineList.Items))
	for i := range machineList.Items {
		machines[i] = &machineList.Items[i]
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

	_, ok := labels[clusterv1.MachineControlPlaneLabel]
	return ok
}

// GetNodeGroupName returns the name of node group that the machine belongs.
func GetNodeGroupName(machine *clusterv1.Machine) string {
	nodeGroupName := ""
	if IsControlPlaneMachine(machine) {
		nodeGroupName = GetKCPNameByMachine(machine)
	} else {
		nodeGroupName = labelsutil.GetDeploymentNameLabel(machine)
	}

	clusterName := labelsutil.GetClusterNameLabelLabel(machine)

	return strings.ReplaceAll(nodeGroupName, fmt.Sprintf("%s-", clusterName), "")
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
	if !IsUUID(uuid) {
		return ""
	}

	return ProviderIDPrefix + uuid
}

func IsUUID(uuid string) bool {
	if uuid == "" {
		return false
	}

	pattern := regexp.MustCompile(UUIDPattern)

	return pattern.MatchString(uuid)
}

func GetNetworkStatus(ipsStr string) []infrav1.NetworkStatus {
	networks := []infrav1.NetworkStatus{}

	if ipsStr == "" {
		return networks
	}

	ips := strings.Split(ipsStr, ",")
	for _, ip := range ips {
		if ip == "127.0.0.1" || strings.HasPrefix(ip, "169.254.") || strings.HasPrefix(ip, "172.17.0") {
			continue
		}

		networks = append(networks, infrav1.NetworkStatus{
			IPAddrs: []string{ip},
		})
	}

	return networks
}
