// Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"io/ioutil"
	"os"
	"testing"

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
	assert.Equal(t, string(defaultCPUTemplate), cfg.CPUTemplate, "expected default CPU template")
}

func TestLoadConfigOverrides(t *testing.T) {
	overrideKernelArgs := "OVERRIDE KERNEL ARGS"
	overrideKernelPath := "OVERRIDE KERNEL PATH"
	overrideRootfsPath := "OVERRIDE ROOTFS PATH"
	overrideCPUCount := 42
	overrideCPUTemplate := "OVERRIDE CPU TEMPLATE"
	configContent := fmt.Sprintf(
		`{
			"kernel_args":"%s",
			"kernel_image_path":"%s",
			"root_drive":"%s",
			"cpu_count": %d,
            "cpu_template": "%s"
		}`, overrideKernelArgs, overrideKernelPath, overrideRootfsPath, overrideCPUCount, overrideCPUTemplate)
	configFile, cleanup := createTempConfig(t, configContent)
	defer cleanup()
	cfg, err := LoadConfig(configFile)
	assert.NoError(t, err, "failed to load config")

	assert.Equal(t, overrideKernelArgs, cfg.KernelArgs, "expected overridden kernel args")
	assert.Equal(t, overrideKernelPath, cfg.KernelImagePath, "expected overridden kernel path")
	assert.Equal(t, overrideRootfsPath, cfg.RootDrive, "expected overridden rootfs path")
	assert.Equal(t, overrideCPUTemplate, cfg.CPUTemplate, "expected overridden CPU template")
}

func createTempConfig(t *testing.T, contents string) (string, func()) {
	t.Helper()
	configFile, err := ioutil.TempFile("", "config")
	if err != nil {
		t.Fatal(err, "failed to create temp config file")
	}
	err = ioutil.WriteFile(configFile.Name(), []byte(contents), 0644)
	if err != nil {
		os.Remove(configFile.Name())
		t.Fatal(err, "failed to write contents to temp config file")
	}
	return configFile.Name(), func() { os.Remove(configFile.Name()) }
}
