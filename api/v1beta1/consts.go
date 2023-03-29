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
)

// Labels.
const (
	// HostServerIDLabel is the label set on nodes.
	// It is the Tower ID of host server where the virtual machine runs on.
	HostServerIDLabel = "cape.infrastructure.cluster.x-k8s.io/host-server-id"

	// HostServerNameLabel is the label set on nodes.
	// It is the name of host server where the virtual machine runs on.
	HostServerNameLabel = "cape.infrastructure.cluster.x-k8s.io/host-server-name"

	// TowerVMIDLabel is the label set on nodes.
	// It is the Tower VM ID.
	TowerVMIDLabel = "cape.infrastructure.cluster.x-k8s.io/tower-vm-id"
)
