/*
Copyright 2025.

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
	"strconv"
	"testing"
	"time"

	"github.com/caarlos0/env/v11"
	. "github.com/onsi/gomega"
)

func TestConfig(t *testing.T) {
	g := NewWithT(t)

	var (
		providerNameShort     = "test"
		defaultRequeueTimeout = 1 * time.Second

		waitTaskInterval                          = 1 * time.Second
		waitTaskTimeout                           = 1 * time.Second
		waitTaskTimeoutForPlacementGroupOperation = 1 * time.Second
		vmPowerStatusCheckingDuration             = 1 * time.Minute

		vmNumCPUs       = int32(1)
		vMMemoryMiB     = int64(1024)
		nameserverLimit = 1
	)

	t.Setenv("PROVIDER_NAME_SHORT", providerNameShort)
	t.Setenv("DEFAULT_REQUEUE_TIMEOUT", defaultRequeueTimeout.String())

	t.Setenv("WAIT_TASK_INTERVAL", waitTaskInterval.String())
	t.Setenv("WAIT_TASK_TIMEOUT", waitTaskTimeout.String())
	t.Setenv("WAIT_TASK_TIMEOUT_FOR_PLACEMENT_GROUP_OPERATION", waitTaskTimeoutForPlacementGroupOperation.String())
	t.Setenv("VM_POWER_STATUS_CHECKING_DURATION", vmPowerStatusCheckingDuration.String())

	t.Setenv("VM_NUM_CPUS", strconv.Itoa(int(vmNumCPUs)))
	t.Setenv("VM_MEMORY_MIB", strconv.Itoa(int(vMMemoryMiB)))
	t.Setenv("VM_NAMESERVER_LIMIT", strconv.Itoa(nameserverLimit))

	g.Expect(env.Parse(&Cape)).To(Succeed())
	g.Expect(env.Parse(&Task)).To(Succeed())
	g.Expect(env.Parse(&VM)).To(Succeed())

	g.Expect(Cape.ProviderNameShort).To(Equal(providerNameShort))
	g.Expect(Cape.DefaultRequeueTimeout).To(Equal(defaultRequeueTimeout))

	g.Expect(Task.WaitTaskInterval).To(Equal(waitTaskInterval))
	g.Expect(Task.WaitTaskTimeoutForPlacementGroupOperation).To(Equal(waitTaskTimeoutForPlacementGroupOperation))
	g.Expect(Task.VMPowerStatusCheckingDuration).To(Equal(vmPowerStatusCheckingDuration))

	g.Expect(VM.VMNumCPUs).To(Equal(vmNumCPUs))
	g.Expect(VM.VMMemoryMiB).To(Equal(vMMemoryMiB))
	g.Expect(VM.NameserverLimit).To(Equal(nameserverLimit))
}
