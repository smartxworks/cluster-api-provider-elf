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

package service

import (
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
)

// GetActualVM returns the real virtual machine.
// When there are multiple virtual machines with the same name,
// the virtual machine with localID in UUID format is the real virtual machine.
func GetActualVM(vms []*models.VM) *models.VM {
	for i := 0; i < len(vms); i++ {
		if util.IsUUID(util.GetTowerString(vms[i].LocalID)) {
			return vms[i]
		}
	}

	return nil
}
