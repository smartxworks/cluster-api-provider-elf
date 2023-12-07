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

package util

import (
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	typesutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/types"
)

// GetVMRef returns the ID or localID of the VM.
// If the localID is in UUID format, return the localID, otherwise return the ID.
//
// Before the ELF VM is created, Tower sets a "placeholder-{UUID}" format string to localID, such as "placeholder-7d8b6df1-c623-4750-a771-3ba6b46995fa".
// After the ELF VM is created, Tower sets the VM ID in UUID format to localID.
func GetVMRef(vm *models.VM) string {
	if vm == nil {
		return ""
	}

	vmLocalID := service.GetTowerString(vm.LocalID)
	if typesutil.IsUUID(vmLocalID) {
		return vmLocalID
	}

	return *vm.ID
}
