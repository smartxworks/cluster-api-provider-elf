/*
Copyright 2026.
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

package hostagent

import (
	"time"

	"github.com/caarlos0/env/v11"
)

// JobTimeoutConfig holds configurable timeouts for host agent jobs.
// A zero job-specific duration falls back to Default, then to defaultTimeout (1m).
type JobTimeoutConfig struct {
	Default time.Duration `env:"HOST_AGENT_JOB_DEFAULT_TIMEOUT"`

	ExpandRootPartition time.Duration `env:"HOST_AGENT_JOB_EXPAND_ROOT_PARTITION_TIMEOUT"`

	RestartKubelet time.Duration `env:"HOST_AGENT_JOB_RESTART_KUBELET_TIMEOUT" envDefault:"5m"`

	SetNetworkDeviceConfig time.Duration `env:"HOST_AGENT_JOB_SET_NETWORK_DEVICE_CONFIG_TIMEOUT"`
}

// JobTimeouts is populated from environment variables in init and may be overridden by CLI flags.
var JobTimeouts JobTimeoutConfig

func init() {
	if err := env.Parse(&JobTimeouts); err != nil {
		panic(err)
	}
}

// TimeoutFor returns the configured timeout for the given job type.
func (c JobTimeoutConfig) TimeoutFor(jobType HostAgentJobType) time.Duration {
	var jobTimeout time.Duration
	switch jobType {
	case HostAgentJobTypeExpandRootPartition:
		jobTimeout = c.ExpandRootPartition
	case HostAgentJobTypeRestartKubelet:
		jobTimeout = c.RestartKubelet
	case HostAgentJobTypeSetNetworkDeviceConfig:
		jobTimeout = c.SetNetworkDeviceConfig
	}
	if jobTimeout > 0 {
		return jobTimeout
	}
	if c.Default > 0 {
		return c.Default
	}
	return defaultTimeout
}
