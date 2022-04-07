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

import "strings"

func IsVMNotFound(err error) bool {
	return err.Error() == "VM_NOT_FOUND"
}

func IsVMDuplicate(err error) bool {
	return strings.Contains(err.Error(), "VM_DUPLICATE")
}
