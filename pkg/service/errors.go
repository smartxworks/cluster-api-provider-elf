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
	"fmt"
	"strings"
)

// error codes.
const (
	ClusterNotFound    = "CLUSTER_NOT_FOUND"
	HostNotFound       = "HOST_NOT_FOUND"
	VMTemplateNotFound = "VM_TEMPLATE_NOT_FOUND"
	VMNotFound         = "VM_NOT_FOUND"
	VMDuplicate        = "VM_DUPLICATE"
	TaskNotFound       = "TASK_NOT_FOUND"
	VlanNotFound       = "VLAN_NOT_FOUND"
	LabelCreateFailed  = "LABEL_CREATE_FAILED"
	LabelAddFailed     = "LABEL_ADD_FAILED"
	CloudInitError     = "VM_CLOUD_INIT_CONFIG_ERROR"
)

func IsVMNotFound(err error) bool {
	return err.Error() == VMNotFound
}

func IsVMDuplicate(err error) bool {
	return strings.Contains(err.Error(), VMDuplicate)
}

func IsShutDownTimeout(message string) bool {
	return strings.Contains(message, "JOB_VM_SHUTDOWN_TIMEOUT")
}

func IsTaskNotFound(err error) bool {
	return err.Error() == TaskNotFound
}

func IsCloudInitConfigError(message string) bool {
	return strings.Contains(message, CloudInitError)
}

// FormatCloudInitError parses useful error message from orignal tower error message.
// Example: The gateway [192.168.31.215] is unreachable.
func FormatCloudInitError(message string) string {
	firstIndex := strings.LastIndex(message, fmt.Sprintf("[%s]", CloudInitError))
	if firstIndex == -1 {
		return message
	}

	msg := message[firstIndex+len(CloudInitError)+2:]
	msg = strings.TrimRight(msg, "}")
	msg = strings.TrimRight(msg, "\"")
	msg = strings.TrimRight(msg, "\\")
	msg = strings.TrimSpace(msg)

	return msg
}
