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

package resources

import (
	"fmt"
)

// Tower labels for VMs created by CAPE.
const (
	VMLabelManaged     = "managed"
	VMLabelNamespace   = "namespace"
	VMLabelClusterName = "cluster-name"
	VMLabelVIP         = "vip"
)

func GetVMLabelManaged() string {
	return fmt.Sprintf("%s-%s", GetResourcePrefix(), VMLabelManaged)
}

func GetVMLabelNamespace() string {
	return fmt.Sprintf("%s-%s", GetResourcePrefix(), VMLabelNamespace)
}

func GetVMLabelClusterName() string {
	return fmt.Sprintf("%s-%s", GetResourcePrefix(), VMLabelClusterName)
}

func GetVMLabelVIP() string {
	return fmt.Sprintf("%s-%s", GetResourcePrefix(), VMLabelVIP)
}
