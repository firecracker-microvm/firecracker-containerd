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

package main

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
	if err != nil {
		t.Error(err, "failed to load config")
	}

	assert.Equal(t, cfg.KernelArgs, defaultKernelArgs, "expected default kernel args")
}

func TestLoadConfigOverrides(t *testing.T) {
	overrideKernelArgs := "OVERRIDE KERNEL ARGS"
	configContent := fmt.Sprintf(`{"kernel_args":"%s"}`, overrideKernelArgs)
	configFile, cleanup := createTempConfig(t, configContent)
	defer cleanup()
	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Error(err, "failed to load config")
	}

	assert.Equal(t, cfg.KernelArgs, overrideKernelArgs, "expected overridden kernel args")
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
