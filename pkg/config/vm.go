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

package config

const (
	// VMDescription is the default description in a VM.
	VMDescription = "Automatically created kubernetes node."

	// VMNumCPUs is the default virtual processors in a VM.
	VMNumCPUs = 2

	// VMMemoryMiB is the default memory in a VM.
	VMMemoryMiB = 2048

	// VMDiskGiB is the default disk size in a VM.
	VMDiskGiB = 10

	// VMDiskName is the default disk name in a VM.
	VMDiskName = "disk"
)
