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

package debug

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		Name           string
		LogLevels      []string
		ExpectedError  error
		ExpectedHelper *Helper
	}{
		{
			Name:           "empty",
			ExpectedHelper: &Helper{},
		},
		{
			Name:          "invalid",
			LogLevels:     []string{"invalid"},
			ExpectedError: NewInvalidLogLevelError("invalid"),
		},
		{
			Name:          "multiple with invalid",
			LogLevels:     []string{LogLevelDebug, LogLevelFirecrackerDebug, "invalid"},
			ExpectedError: NewInvalidLogLevelError("invalid"),
		},
		{
			Name:      "debug",
			LogLevels: []string{LogLevelDebug},
			ExpectedHelper: &Helper{
				logDebug: true,
			},
		},
		{
			Name:      "error",
			LogLevels: []string{LogLevelError},
			ExpectedHelper: &Helper{
				logError: true,
			},
		},
		{
			Name:      "info",
			LogLevels: []string{LogLevelInfo},
			ExpectedHelper: &Helper{
				logInfo: true,
			},
		},
		{
			Name:      "warning",
			LogLevels: []string{LogLevelWarning},
			ExpectedHelper: &Helper{
				logWarning: true,
			},
		},
		{
			Name:          "multiple top log levels",
			LogLevels:     []string{LogLevelDebug, LogLevelError, LogLevelInfo, LogLevelWarning},
			ExpectedError: ErrLogLevelAlreadySet,
		},
		{
			Name:      "firecracker debug",
			LogLevels: []string{LogLevelFirecrackerDebug},
			ExpectedHelper: &Helper{
				logFC: "Debug",
			},
		},
		{
			Name:      "firecracker error",
			LogLevels: []string{LogLevelFirecrackerError},
			ExpectedHelper: &Helper{
				logFC: "Error",
			},
		},
		{
			Name:      "firecracker info",
			LogLevels: []string{LogLevelFirecrackerInfo},
			ExpectedHelper: &Helper{
				logFC: "Info",
			},
		},
		{
			Name:      "firecracker warning",
			LogLevels: []string{LogLevelFirecrackerWarning},
			ExpectedHelper: &Helper{
				logFC: "Warning",
			},
		},
		{
			Name:      "firecracker output",
			LogLevels: []string{LogLevelFirecrackerOutput},
			ExpectedHelper: &Helper{
				logFCOutput: true,
			},
		},
		{
			Name:          "multiple firecracker log levels",
			LogLevels:     []string{LogLevelFirecrackerWarning, LogLevelFirecrackerDebug, LogLevelFirecrackerError},
			ExpectedError: ErrFCLogLevelAlreadySet,
		},
		{
			Name:      "firecracker-go-sdk debug level",
			LogLevels: []string{LogLevelFirecrackerSDKDebug},
			ExpectedHelper: &Helper{
				fcSDKLogLevelSet: true,
				logFCSDK:         logrus.DebugLevel,
			},
		},
		{
			Name:      "firecracker-go-sdk error level",
			LogLevels: []string{LogLevelFirecrackerSDKError},
			ExpectedHelper: &Helper{
				fcSDKLogLevelSet: true,
				logFCSDK:         logrus.ErrorLevel,
			},
		},
		{
			Name:      "firecracker-go-sdk info level",
			LogLevels: []string{LogLevelFirecrackerSDKInfo},
			ExpectedHelper: &Helper{
				fcSDKLogLevelSet: true,
				logFCSDK:         logrus.InfoLevel,
			},
		},
		{
			Name:      "firecracker-go-sdk warning level",
			LogLevels: []string{LogLevelFirecrackerSDKWarning},
			ExpectedHelper: &Helper{
				fcSDKLogLevelSet: true,
				logFCSDK:         logrus.WarnLevel,
			},
		},
		{
			Name:      "firecracker-go-sdk multiple levels",
			LogLevels: []string{LogLevelDebug, LogLevelFirecrackerSDKWarning},
			ExpectedHelper: &Helper{
				logFC:                   "Debug",
				fcSDKLogLevelSet:        true,
				logFCSDK:                logrus.WarnLevel,
				fcContainerdLogLevelSet: true,
				logFCContainerd:         logrus.DebugLevel,
			},
		},
		{
			Name:          "firecracker-go-sdk multiple levels",
			LogLevels:     []string{LogLevelDebug, LogLevelFirecrackerSDKWarning, LogLevelFirecrackerSDKDebug},
			ExpectedError: ErrFCSDKLogLevelAlreadySet,
		},
		{
			Name:      "firecracker containerd debug",
			LogLevels: []string{LogLevelFirecrackerContainerdDebug},
			ExpectedHelper: &Helper{
				fcContainerdLogLevelSet: true,
				logFCContainerd:         logrus.DebugLevel,
			},
		},
		{
			Name:      "firecracker containerd error",
			LogLevels: []string{LogLevelFirecrackerContainerdError},
			ExpectedHelper: &Helper{
				fcContainerdLogLevelSet: true,
				logFCContainerd:         logrus.ErrorLevel,
			},
		},
		{
			Name:      "firecracker containerd info",
			LogLevels: []string{LogLevelFirecrackerContainerdInfo},
			ExpectedHelper: &Helper{
				fcContainerdLogLevelSet: true,
				logFCContainerd:         logrus.InfoLevel,
			},
		},
		{
			Name:      "firecracker containerd warning",
			LogLevels: []string{LogLevelFirecrackerContainerdWarning},
			ExpectedHelper: &Helper{
				fcContainerdLogLevelSet: true,
				logFCContainerd:         logrus.WarnLevel,
			},
		},
		{
			Name:          "multiple firecracker containerd log levels",
			LogLevels:     []string{LogLevelFirecrackerContainerdWarning, LogLevelFirecrackerContainerdDebug, LogLevelFirecrackerContainerdError},
			ExpectedError: ErrFCContainerdLogLevelAlreadySet,
		},
	}

	for _, _c := range cases {
		c := _c

		t.Run(c.Name, func(t *testing.T) {
			h, err := New(c.LogLevels...)
			require.Equalf(t, c.ExpectedError, err, "expected errors to be equal")
			if c.ExpectedError != nil {
				return
			}

			require.NotNilf(t, h, "expected Helper to be non-nil")

			assert.Equal(t, c.ExpectedHelper.GetFirecrackerLogLevel(), h.GetFirecrackerLogLevel())
			expectedLogLevel, expectedOk := c.ExpectedHelper.GetFirecrackerSDKLogLevel()
			logLevel, ok := h.GetFirecrackerSDKLogLevel()
			assert.Equal(t, expectedOk, ok)
			assert.Equal(t, expectedLogLevel, logLevel)
			expectedLogLevel, expectedOk = c.ExpectedHelper.GetFirecrackerContainerdLogLevel()
			logLevel, ok = h.GetFirecrackerContainerdLogLevel()
			assert.Equal(t, expectedOk, ok)
			assert.Equal(t, expectedLogLevel, logLevel)
		})
	}
}
