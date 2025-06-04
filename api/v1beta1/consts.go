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

import (
	"time"
)

const (
	// VMDisconnectionTimeout is the time allowed for the virtual machine to be disconnected.
	// The virtual machine will be marked as deleted after the timeout.
	VMDisconnectionTimeout = 1 * time.Minute
)

// Annotations.
const (
	// PlacementGroupNameAnnotation is the annotation identifying the name of placement group.
	PlacementGroupNameAnnotation = "cape.infrastructure.cluster.x-k8s.io/placement-group-name"

	// CAPEVersionAnnotation is the annotation identifying the version of CAPE that the resource reconciled by.
	CAPEVersionAnnotation = "cape.infrastructure.cluster.x-k8s.io/cape-version"

	// CreatedByAnnotation is the annotation identifying the creator of the resource.
	//
	// The creator can be in one of the following two formats:
	// 1. ${Tower username}@${Tower auth_config_id}, e.g. caas.smartx@7e98ecbb-779e-43f6-8330-1bc1d29fffc7.
	// 2. ${Tower username}, e.g. root. If auth_config_id is not set, it means it is a LOCAL user.
	CreatedByAnnotation = "cape.infrastructure.cluster.x-k8s.io/created-by"
)

// Labels.
const (
	// HostServerIDLabel is the label set on nodes.
	// It is the Tower ID of host server where the virtual machine runs on.
	HostServerIDLabel = "cape.infrastructure.cluster.x-k8s.io/host-server-id"

	// HostServerNameLabel is the label set on nodes.
	// It is the name of host server where the virtual machine runs on.
	HostServerNameLabel = "cape.infrastructure.cluster.x-k8s.io/host-server-name"

	// ZoneIDLabel is the label set on nodes.
	// It is the Tower ID of zone where the virtual machine runs on.
	ZoneIDLabel = "cape.infrastructure.cluster.x-k8s.io/zone-id"

	// ZoneTypeLabel is the label set on nodes.
	// It is the type of zone where the virtual machine runs on.
	ZoneTypeLabel = "cape.infrastructure.cluster.x-k8s.io/zone-type"

	// TowerVMIDLabel is the label set on nodes.
	// It is the Tower VM ID.
	TowerVMIDLabel = "cape.infrastructure.cluster.x-k8s.io/tower-vm-id"

	// NodeGroupLabel is the label set on nodes.
	// It is the node group name.
	// Node group name of CP node parses from KCP name,
	// and worker node parses from MD name.
	NodeGroupLabel = "cape.infrastructure.cluster.x-k8s.io/node-group"
)
