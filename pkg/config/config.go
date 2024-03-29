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

import "time"

var (
	ProviderNameShort = "cape"

	// DefaultRequeueTimeout is the default time for how long to wait when
	// requeueing a CAPE operation.
	DefaultRequeueTimeout = 10 * time.Second

	// WaitTaskInterval is the default interval time polling task.
	WaitTaskInterval = 1 * time.Second

	// WaitTaskTimeout is the default timeout for waiting for task to complete.
	WaitTaskTimeout = 3 * time.Second

	// WaitTaskTimeoutForPlacementGroupOperation is the timeout for waiting for placement group creating/updating/deleting task to complete.
	WaitTaskTimeoutForPlacementGroupOperation = 10 * time.Second

	// VMPowerStatusCheckingDuration is the time duration for cheking if the VM is powered off
	// after the Machine's NodeHealthy condition status is set to Unknown.
	VMPowerStatusCheckingDuration = 2 * time.Minute
)
