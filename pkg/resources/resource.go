/*
Copyright 2023.

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

import "github.com/smartxworks/cluster-api-provider-elf/pkg/util"

// Tower resources.
const (
	TowerResourcePrefix = "TOWER_RESOURCE_PREFIX"

	// By default, CAPE allow modify the configuration(CPU) of VM directly.
	// If you want to limit the VM configuration to be modified directly,
	// set AllowCustomVMConfig to false, CAPE will try to restore the VM configuration
	// set by ElfMachine.
	AllowCustomVMConfig = "ALLOW_CUSTOM_VM_CONFIG"
)

func GetResourcePrefix() string {
	return util.GetEnv(TowerResourcePrefix, "cape")
}

func IsAllowCustomVMConfig() bool {
	return util.GetEnv(AllowCustomVMConfig, "true") == "false"
}
