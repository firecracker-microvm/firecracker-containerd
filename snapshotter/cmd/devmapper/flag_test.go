// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseKeyValueOpt(t *testing.T) {
	key, value, err := parseKeyValueOpt("dm.baseSize=1")
	assert.NoError(t, err)
	assert.Equal(t, "dm.baseSize", key)
	assert.Equal(t, "1", value)

	key, value, err = parseKeyValueOpt(" dm.device = /dev/mapper/test ")
	assert.NoError(t, err)
	assert.Equal(t, "dm.device", key)
	assert.Equal(t, "/dev/mapper/test", value)

	key, value, err = parseKeyValueOpt(" dm.device =   ")
	assert.NoError(t, err)
	assert.Equal(t, "dm.device", key)
	assert.Equal(t, "", value)
}

func TestParseInvalidKeyValueOpt(t *testing.T) {
	_, _, err := parseKeyValueOpt("dm.baseSize")
	assert.Error(t, err)

	_, _, err = parseKeyValueOpt("=value")
	assert.Error(t, err)
}

func TestVisitKeyValueOpts(t *testing.T) {
	var (
		args   = os.Args
		called = false
	)

	os.Args = []string{"exec_path", "--opt", "a=b"}
	err := visitKeyValueOpts("--opt", func(key, value string) error {
		called = true
		assert.Equal(t, "a", key)
		assert.Equal(t, "b", value)
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, called)

	defer func() { os.Args = args }()
}

func TestVisitKeyValueOptCallbackErr(t *testing.T) {
	var (
		args  = os.Args
		count = 0
		cbErr = errors.New("test")
	)

	os.Args = []string{"exec_path", "--opt", "a=b", "--opt", "c=d"}
	err := visitKeyValueOpts("--opt", func(key, value string) error {
		count++
		return cbErr
	})

	assert.Equal(t, cbErr, err)
	assert.Equalf(t, 1, count, "callback should be called exactly once")

	defer func() { os.Args = args }()
}

func TestVisitInvalidOpts(t *testing.T) {
	args := os.Args

	tests := []struct {
		name string
		args []string
		err  error
	}{
		{"Empty args", []string{}, nil},
		{"Nil", nil, nil},
		{"Empty string", []string{""}, nil},
		{"Empty strings", []string{"", "", "", "", "", ""}, nil},
		{"Just reboot flag", []string{"-reboot"}, nil},
		{"Single opt flag", []string{"--opt"}, nil},
		{"Opt flag at end", []string{"--type", "devmapper", "--opt"}, nil},
		{"Opt flag at start", []string{"--opt", "--type", "devmapper"}, errors.New("failed to split option: \"--type\"")},
		{"Opt with invalid kv pair", []string{"--opt", "1+1"}, errors.New("failed to split option: \"1+1\"")},
		{"Opt with empty kv", []string{"--opt", ""}, errors.New("failed to split option: \"\"")},
	}

	for _, test := range tests {
		var (
			expectedErr = test.err
			args        = test.args
		)

		t.Run(test.name, func(t *testing.T) {
			os.Args = append([]string{"exec_path_to_be_skipped"}, args...)
			err := visitKeyValueOpts("--opt", func(_, _ string) error {
				assert.Fail(t, "callback should not be called")
				return nil
			})

			if expectedErr == nil {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, expectedErr.Error())
			}
		})
	}

	defer func() { os.Args = args }()
}
