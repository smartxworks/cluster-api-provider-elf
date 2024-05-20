/*
Copyright 2024.
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

package tasks

import (
	_ "embed"
)

// ExpandRootPartitionTask is the task to add new disk capacity to root.
//
//go:embed expand_root_partition.yaml
var ExpandRootPartitionTask string

// RestartKubeletTask is the task to restart kubelet.
//
//go:embed restart_kubelet.yaml
var RestartKubeletTask string

// SetNetworkDeviceConfig is the task to set network device configuration.
//
//go:embed set_network_device_config.yaml
var SetNetworkDeviceConfig string
