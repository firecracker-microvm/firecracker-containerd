// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package config

import (
	"fmt"
	"os"
	"testing"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfigDefaults(t *testing.T) {
	configContent := `{}`
	configFile, cleanup := createTempConfig(t, configContent)
	defer cleanup()
	cfg, err := LoadConfig(configFile)
	assert.NoError(t, err, "failed to load config")

	assert.Equal(t, defaultKernelArgs, cfg.KernelArgs, "expected default kernel args")
	assert.Equal(t, defaultKernelPath, cfg.KernelImagePath, "expected default kernel path")
	assert.Equal(t, defaultRootfsPath, cfg.RootDrive, "expected default rootfs path")
}

func TestLoadConfigOverrides(t *testing.T) {
	overrideKernelArgs := "OVERRIDE KERNEL ARGS"
	overrideKernelPath := "OVERRIDE KERNEL PATH"
	overrideRootfsPath := "OVERRIDE ROOTFS PATH"
	overrideCPUTemplate := ""
	if cpuTemp, err := internal.SupportCPUTemplate(); cpuTemp && err == nil {
		overrideCPUTemplate = "OVERRIDE CPU TEMPLATE"
	}
	configContent := fmt.Sprintf(
		`{
			"kernel_args":"%s",
			"kernel_image_path":"%s",
			"root_drive":"%s",
			"cpu_template": "%s",
			"log_levels": ["debug"]
		}`, overrideKernelArgs, overrideKernelPath, overrideRootfsPath, overrideCPUTemplate)
	configFile, cleanup := createTempConfig(t, configContent)
	defer cleanup()
	cfg, err := LoadConfig(configFile)
	assert.NoError(t, err, "failed to load config")

	assert.Equal(t, overrideKernelArgs, cfg.KernelArgs, "expected overridden kernel args")
	assert.Equal(t, overrideKernelPath, cfg.KernelImagePath, "expected overridden kernel path")
	assert.Equal(t, overrideRootfsPath, cfg.RootDrive, "expected overridden rootfs path")
	assert.Equal(t, overrideCPUTemplate, cfg.CPUTemplate, "expected overridden CPU template")

	assert.True(t, cfg.DebugHelper.LogFirecrackerOutput())
}

func createTempConfig(t *testing.T, contents string) (string, func()) {
	t.Helper()
	configFile, err := os.CreateTemp("", "config")
	if err != nil {
		t.Fatal(err, "failed to create temp config file")
	}
	err = os.WriteFile(configFile.Name(), []byte(contents), 0644)
	if err != nil {
		os.Remove(configFile.Name())
		t.Fatal(err, "failed to write contents to temp config file")
	}
	return configFile.Name(), func() { os.Remove(configFile.Name()) }
}

func TestLoadConfigDefaultVcpuAndMem(t *testing.T) {
	testcases := []struct {
		name          string
		configContent string
		expectedVcpu  uint32
		expectedMem   uint32
	}{
		{
			name:          "DefaultsWhenNotSpecified",
			configContent: `{}`,
			expectedVcpu:  0, // 0 means use hardcoded default
			expectedMem:   0, // 0 means use hardcoded default
		},
		{
			name:          "CustomVcpuAndMem",
			configContent: `{"default_vcpu_count": 4, "default_mem_size_mib": 512}`,
			expectedVcpu:  4,
			expectedMem:   512,
		},
		{
			name:          "LargeVcpuCount",
			configContent: `{"default_vcpu_count": 32}`,
			expectedVcpu:  32,
			expectedMem:   0,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			configFile, cleanup := createTempConfig(t, tc.configContent)
			defer cleanup()

			cfg, err := LoadConfig(configFile)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedVcpu, cfg.DefaultVcpuCount)
			assert.Equal(t, tc.expectedMem, cfg.DefaultMemSizeMib)
		})
	}
}
