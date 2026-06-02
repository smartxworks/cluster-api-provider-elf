/*
Copyright 2024.
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
	"testing"
	"time"

	"github.com/caarlos0/env/v11"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
)

var jobTimeoutEnvKeys = []string{
	"HOST_AGENT_JOB_DEFAULT_TIMEOUT",
	"HOST_AGENT_JOB_EXPAND_ROOT_PARTITION_TIMEOUT",
	"HOST_AGENT_JOB_RESTART_KUBELET_TIMEOUT",
	"HOST_AGENT_JOB_SET_NETWORK_DEVICE_CONFIG_TIMEOUT",
}

func unsetJobTimeoutEnv(t *testing.T) {
	t.Helper()
	for _, key := range jobTimeoutEnvKeys {
		t.Setenv(key, "")
	}
}

func parseJobTimeoutEnv(t *testing.T) JobTimeoutConfig {
	t.Helper()
	var timeouts JobTimeoutConfig
	g := NewWithT(t)
	g.Expect(env.Parse(&timeouts)).To(Succeed())
	return timeouts
}

func TestJobTimeoutConfigTimeoutFor(t *testing.T) {
	tests := []struct {
		name    string
		config  JobTimeoutConfig
		jobType HostAgentJobType
		want    time.Duration
	}{
		{
			name:    "uses built-in default when all values are zero",
			config:  JobTimeoutConfig{},
			jobType: HostAgentJobTypeExpandRootPartition,
			want:    defaultTimeout,
		},
		{
			name: "uses configured default for all job types",
			config: JobTimeoutConfig{
				Default: 2 * time.Minute,
			},
			jobType: HostAgentJobTypeRestartKubelet,
			want:    2 * time.Minute,
		},
		{
			name: "job-specific timeout overrides default",
			config: JobTimeoutConfig{
				Default:             2 * time.Minute,
				ExpandRootPartition: 3 * time.Minute,
			},
			jobType: HostAgentJobTypeExpandRootPartition,
			want:    3 * time.Minute,
		},
		{
			name: "other job types still use default when one job is overridden",
			config: JobTimeoutConfig{
				Default:             2 * time.Minute,
				ExpandRootPartition: 3 * time.Minute,
			},
			jobType: HostAgentJobTypeRestartKubelet,
			want:    2 * time.Minute,
		},
		{
			name: "uses job-specific timeout without default",
			config: JobTimeoutConfig{
				SetNetworkDeviceConfig: 5 * time.Minute,
			},
			jobType: HostAgentJobTypeSetNetworkDeviceConfig,
			want:    5 * time.Minute,
		},
		{
			name: "unknown job type falls back to configured default",
			config: JobTimeoutConfig{
				Default: 2 * time.Minute,
			},
			jobType: HostAgentJobType("unknown"),
			want:    2 * time.Minute,
		},
		{
			name:    "unknown job type falls back to built-in default",
			config:  JobTimeoutConfig{},
			jobType: HostAgentJobType("unknown"),
			want:    defaultTimeout,
		},
		{
			name: "restart-kubelet uses env default when unset",
			config: JobTimeoutConfig{
				RestartKubelet: 5 * time.Minute,
			},
			jobType: HostAgentJobTypeRestartKubelet,
			want:    5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(tt.config.TimeoutFor(tt.jobType)).To(Equal(tt.want))
		})
	}
}

func TestJobTimeoutConfigFromEnv(t *testing.T) {
	t.Run("parses all job timeout environment variables", func(t *testing.T) {
		g := NewWithT(t)
		unsetJobTimeoutEnv(t)

		t.Setenv("HOST_AGENT_JOB_DEFAULT_TIMEOUT", "2m")
		t.Setenv("HOST_AGENT_JOB_EXPAND_ROOT_PARTITION_TIMEOUT", "3m")
		t.Setenv("HOST_AGENT_JOB_RESTART_KUBELET_TIMEOUT", "4m")
		t.Setenv("HOST_AGENT_JOB_SET_NETWORK_DEVICE_CONFIG_TIMEOUT", "5m")

		timeouts := parseJobTimeoutEnv(t)
		g.Expect(timeouts.Default).To(Equal(2 * time.Minute))
		g.Expect(timeouts.ExpandRootPartition).To(Equal(3 * time.Minute))
		g.Expect(timeouts.RestartKubelet).To(Equal(4 * time.Minute))
		g.Expect(timeouts.SetNetworkDeviceConfig).To(Equal(5 * time.Minute))
	})

	t.Run("uses default timeout for unset job types", func(t *testing.T) {
		g := NewWithT(t)
		unsetJobTimeoutEnv(t)

		t.Setenv("HOST_AGENT_JOB_DEFAULT_TIMEOUT", "2m")

		timeouts := parseJobTimeoutEnv(t)
		g.Expect(timeouts.TimeoutFor(HostAgentJobTypeExpandRootPartition)).To(Equal(2 * time.Minute))
		g.Expect(timeouts.TimeoutFor(HostAgentJobTypeRestartKubelet)).To(Equal(5 * time.Minute))
		g.Expect(timeouts.TimeoutFor(HostAgentJobTypeSetNetworkDeviceConfig)).To(Equal(2 * time.Minute))
	})

	t.Run("uses job-specific timeout without default env", func(t *testing.T) {
		g := NewWithT(t)
		unsetJobTimeoutEnv(t)

		t.Setenv("HOST_AGENT_JOB_RESTART_KUBELET_TIMEOUT", "4m")

		timeouts := parseJobTimeoutEnv(t)
		g.Expect(timeouts.Default).To(BeZero())
		g.Expect(timeouts.TimeoutFor(HostAgentJobTypeRestartKubelet)).To(Equal(4 * time.Minute))
		g.Expect(timeouts.TimeoutFor(HostAgentJobTypeExpandRootPartition)).To(Equal(defaultTimeout))
	})

	t.Run("returns zero values when environment variables are unset", func(t *testing.T) {
		g := NewWithT(t)
		unsetJobTimeoutEnv(t)

		timeouts := parseJobTimeoutEnv(t)
		g.Expect(timeouts).To(Equal(JobTimeoutConfig{
			RestartKubelet: 5 * time.Minute,
		}))
	})
}

func TestJobTimeoutConfigFromEnvInvalid(t *testing.T) {
	g := NewWithT(t)
	unsetJobTimeoutEnv(t)

	t.Setenv("HOST_AGENT_JOB_DEFAULT_TIMEOUT", "not-a-duration")

	var timeouts JobTimeoutConfig
	g.Expect(env.Parse(&timeouts)).NotTo(Succeed())
}

func TestJobTimeoutConfigCLIFlag(t *testing.T) {
	t.Run("preserves env value when CLI flag is not provided", func(t *testing.T) {
		g := NewWithT(t)

		var timeout = 3 * time.Minute
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.DurationVar(&timeout, "host-agent-job-default-timeout", timeout, "timeout")

		g.Expect(fs.Parse([]string{})).To(Succeed())
		g.Expect(timeout).To(Equal(3 * time.Minute))
	})

	t.Run("overrides env value when CLI flag is provided", func(t *testing.T) {
		g := NewWithT(t)

		var timeout = 3 * time.Minute
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.DurationVar(&timeout, "host-agent-job-default-timeout", timeout, "timeout")

		g.Expect(fs.Parse([]string{"--host-agent-job-default-timeout=5m"})).To(Succeed())
		g.Expect(timeout).To(Equal(5 * time.Minute))
	})

	t.Run("preserves per-job env values when only one CLI flag is provided", func(t *testing.T) {
		g := NewWithT(t)

		timeouts := JobTimeoutConfig{
			Default:             2 * time.Minute,
			ExpandRootPartition: 3 * time.Minute,
			RestartKubelet:      4 * time.Minute,
		}

		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.DurationVar(&timeouts.Default, "host-agent-job-default-timeout", timeouts.Default, "default timeout")
		fs.DurationVar(&timeouts.ExpandRootPartition, "host-agent-job-expand-root-partition-timeout", timeouts.ExpandRootPartition, "expand timeout")
		fs.DurationVar(&timeouts.RestartKubelet, "host-agent-job-restart-kubelet-timeout", timeouts.RestartKubelet, "restart timeout")

		g.Expect(fs.Parse([]string{"--host-agent-job-restart-kubelet-timeout=6m"})).To(Succeed())
		g.Expect(timeouts.Default).To(Equal(2 * time.Minute))
		g.Expect(timeouts.ExpandRootPartition).To(Equal(3 * time.Minute))
		g.Expect(timeouts.RestartKubelet).To(Equal(6 * time.Minute))
		g.Expect(timeouts.TimeoutFor(HostAgentJobTypeRestartKubelet)).To(Equal(6 * time.Minute))
		g.Expect(timeouts.TimeoutFor(HostAgentJobTypeExpandRootPartition)).To(Equal(3 * time.Minute))
	})
}
