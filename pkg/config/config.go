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

import (
	"time"

	"github.com/caarlos0/env/v11"
)

var (
	Task taskConfig
	Cape capeConfig
	VM   vmConfig
)

type capeConfig struct {
	ProviderNameShort string `env:"PROVIDER_NAME_SHORT" envDefault:"cape"`

	// DefaultRequeueTimeout is the default time for how long to wait when
	// requeueing a CAPE operation.
	DefaultRequeueTimeout time.Duration `env:"DEFAULT_REQUEUE_TIMEOUT" envDefault:"10s"`
}

type taskConfig struct {
	// WaitTaskInterval is the default interval time polling task.
	WaitTaskInterval time.Duration `env:"WAIT_TASK_INTERVAL" envDefault:"1s"`

	// WaitTaskTimeoutForPlacementGroupOperation is the timeout for waiting for placement group creating/updating/deleting task to complete.
	WaitTaskTimeoutForPlacementGroupOperation time.Duration `env:"WAIT_TASK_TIMEOUT_FOR_PLACEMENT_GROUP_OPERATION" envDefault:"10s"`

	// VMPowerStatusCheckingDuration is the time duration for cheking if the VM is powered off
	// after the Machine's NodeHealthy condition status is set to Unknown.
	VMPowerStatusCheckingDuration time.Duration `env:"VM_POWER_STATUS_CHECKING_DURATION" envDefault:"2m"`
}

type vmConfig struct {
	// VMNumCPUs is the default virtual processors in a VM.
	VMNumCPUs int32 `env:"VM_NUM_CPUS" envDefault:"2"`

	// VMMemoryMiB is the default memory in a VM.
	VMMemoryMiB int64 `env:"VM_MEMORY_MIB" envDefault:"2048"`

	// NameserverLimit is the maximum number of nameservers allowed in a VM.
	NameserverLimit int `env:"VM_NAMESERVER_LIMIT" envDefault:"3"`
}

func init() {
	if err := env.Parse(&Cape); err != nil {
		panic(err)
	}
	if err := env.Parse(&Task); err != nil {
		panic(err)
	}
	if err := env.Parse(&VM); err != nil {
		panic(err)
	}
}
