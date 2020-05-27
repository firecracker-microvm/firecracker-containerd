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
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	// LogLevelDebug will enable debugging for all applciations and SDKs. This
	// uses log levels of firecracker:debug, firecracker-go-sdk:debug,
	// firecracker:output, and firecracker-containerd:debug
	LogLevelDebug = "debug"
	// LogLevelError will enable error logging for all applciations and SDKs.
	// This uses log levels of firecracker:error, firecracker-go-sdk:error,
	// and firecracker-containerd:error
	LogLevelError = "error"
	// LogLevelInfo will enable info logging for all applciations and SDKs.
	// This uses log levels of firecracker:info, firecracker-go-sdk:info,
	// and firecracker-containerd:info
	LogLevelInfo = "info"
	// LogLevelWarning will enable warning logging for all applciations and SDKs.
	// This uses log levels of firecracker:warning, firecracker-go-sdk:warning,
	// firecracker:output, and firecracker-containerd:warning
	LogLevelWarning = "warning"
	// LogLevelFirecrackerDebug enables debug level logging on Firecracker
	// MicroVMs
	LogLevelFirecrackerDebug = "firecracker:debug"
	// LogLevelFirecrackerError enables error level logging on Firecracker
	// MicroVMs
	LogLevelFirecrackerError = "firecracker:error"
	// LogLevelFirecrackerInfo enables info level logging on Firecracker MicroVMs
	LogLevelFirecrackerInfo = "firecracker:info"
	// LogLevelFirecrackerWarning enables warning level logging on Firecracker
	// MicroVMs
	LogLevelFirecrackerWarning = "firecracker:warning"
	// LogLevelFirecrackerOutput will redirect stdout and stderr of Firecracker
	// to logging. By default, stdout and stderr is ignored
	LogLevelFirecrackerOutput = "firecracker:output"
	// LogLevelFirecrackerSDKDebug enables debug level logging on
	// firecracker-go-sdk
	LogLevelFirecrackerSDKDebug = "firecracker-go-sdk:debug"
	// LogLevelFirecrackerSDKError enables error level logging on
	// firecracker-go-sdk
	LogLevelFirecrackerSDKError = "firecracker-go-sdk:error"
	// LogLevelFirecrackerSDKInfo enables info level logging on
	// firecracker-go-sdk
	LogLevelFirecrackerSDKInfo = "firecracker-go-sdk:info"
	// LogLevelFirecrackerSDKWarning enables warning level logging on
	// firecracker-go-sdk
	LogLevelFirecrackerSDKWarning = "firecracker-go-sdk:warning"
	// LogLevelFirecrackerContainerdDebug enables debug level logging on
	// firecracker-containerd
	LogLevelFirecrackerContainerdDebug = "firecracker-containerd:debug"
	// LogLevelFirecrackerContainerdError enables error level logging on
	// firecracker-containerd
	LogLevelFirecrackerContainerdError = "firecracker-containerd:error"
	// LogLevelFirecrackerContainerdInfo enables info level logging on
	// firecracker-containerd
	LogLevelFirecrackerContainerdInfo = "firecracker-containerd:info"
	// LogLevelFirecrackerContainerdWarning enables warning level logging on
	// firecracker-containerd
	LogLevelFirecrackerContainerdWarning = "firecracker-containerd:warning"
)

// ErrLogLevelAlreadySet will return if a log level has been previously set.
var ErrLogLevelAlreadySet = fmt.Errorf("only one value for top level log level can be set")

// ErrFCLogLevelAlreadySet will return if a log level has been previously set.
var ErrFCLogLevelAlreadySet = fmt.Errorf("only one value of firecracker log level can be set")

// ErrFCSDKLogLevelAlreadySet will return if a log level has been previously set.
var ErrFCSDKLogLevelAlreadySet = fmt.Errorf("only one value of firecracker-go-sdk log level can be set")

// ErrFCContainerdLogLevelAlreadySet will return if a log level has been previously set.
var ErrFCContainerdLogLevelAlreadySet = fmt.Errorf("only one value of firecracker-containerd log level can be set")

// Helper is used to abstract away the complications of multilevel log levels.
type Helper struct {
	logLevels []string
	ShimDebug bool

	logDebug        bool
	logError        bool
	logInfo         bool
	logWarning      bool
	logFCSDK        logrus.Level
	logFCOutput     bool
	logFC           string
	logFCContainerd logrus.Level

	fcSDKLogLevelSet        bool
	fcContainerdLogLevelSet bool
}

// New will return a new Helper in the event an error does not occur. This will
// parse the logLevel provided to figure out what logging is enabled. New will
// also validate that the log level is a valid value along with any mutually
// exclusive values.
func New(logLevels ...string) (*Helper, error) {
	h := &Helper{
		logLevels: logLevels,
	}

	if err := h.setLogLevels(logLevels); err != nil {
		return nil, err
	}

	return h, nil
}

// GetFirecrackerLogLevel will return the current log level for Firecracker.
func (h *Helper) GetFirecrackerLogLevel() string {
	if h.logFC != "" {
		return h.logFC
	}

	if h.logDebug {
		return "Debug"
	}

	if h.logError {
		return "Error"
	}

	if h.logInfo {
		return "Info"
	}

	if h.logWarning {
		return "Warning"
	}

	return h.logFC
}

// LogFirecrackerOutput will return whether our not we want to redirect stdout
// and stderr to our logging
func (h *Helper) LogFirecrackerOutput() bool {
	return h.logDebug || h.logFCOutput
}

// GetFirecrackerSDKLogLevel returns what log level to log the
// firecracker-go-sdk at. This also return whether or not the value was set
func (h *Helper) GetFirecrackerSDKLogLevel() (logrus.Level, bool) {
	if h.fcSDKLogLevelSet {
		return h.logFCSDK, true
	}

	if h.logDebug {
		return logrus.DebugLevel, true
	}

	if h.logError {
		return logrus.ErrorLevel, true
	}

	if h.logInfo {
		return logrus.InfoLevel, true
	}

	if h.logWarning {
		return logrus.WarnLevel, true
	}

	return h.logFCSDK, false
}

// GetFirecrackerContainerdLogLevel return the log level for
// firecracker-containerd
func (h *Helper) GetFirecrackerContainerdLogLevel() (logrus.Level, bool) {
	if h.fcContainerdLogLevelSet {
		return h.logFCContainerd, h.fcContainerdLogLevelSet
	}

	if h.logDebug || h.ShimDebug {
		return logrus.DebugLevel, true
	}

	if h.logError {
		return logrus.ErrorLevel, true
	}

	if h.logInfo {
		return logrus.InfoLevel, true
	}

	if h.logWarning {
		return logrus.WarnLevel, true
	}

	return logrus.PanicLevel, false
}

func (h *Helper) setFirecrackerLogLevel(level string) error {
	if h.logFC != "" {
		return ErrFCLogLevelAlreadySet
	}

	h.logFC = level
	return nil
}

func (h *Helper) setFirecrackerContainerdLogLevel(level logrus.Level) error {
	if h.fcContainerdLogLevelSet {
		return ErrFCContainerdLogLevelAlreadySet
	}

	h.fcContainerdLogLevelSet = true
	h.logFCContainerd = level
	return nil
}

func (h *Helper) setFirecrackerSDKLogLevel(level logrus.Level) error {
	if h.fcSDKLogLevelSet {
		return ErrFCSDKLogLevelAlreadySet
	}
	h.fcSDKLogLevelSet = true
	h.logFCSDK = level
	return nil
}

func (h *Helper) isTopLogLevelSet() bool {
	return h.logDebug || h.logError || h.logInfo || h.logWarning
}

func (h *Helper) setLogLevels(logLevels []string) error {
	if len(logLevels) == 0 {
		return nil
	}

	for _, level := range logLevels {
		cleanedLevel := strings.TrimSpace(level)

		switch cleanedLevel {
		case LogLevelDebug:
			if h.isTopLogLevelSet() {
				return ErrLogLevelAlreadySet
			}
			h.logDebug = true
		case LogLevelError:
			if h.isTopLogLevelSet() {
				return ErrLogLevelAlreadySet
			}
			h.logError = true
		case LogLevelInfo:
			if h.isTopLogLevelSet() {
				return ErrLogLevelAlreadySet
			}
			h.logInfo = true
		case LogLevelWarning:
			if h.isTopLogLevelSet() {
				return ErrLogLevelAlreadySet
			}
			h.logWarning = true
		case LogLevelFirecrackerDebug:
			if err := h.setFirecrackerLogLevel("Debug"); err != nil {
				return err
			}
		case LogLevelFirecrackerError:
			if err := h.setFirecrackerLogLevel("Error"); err != nil {
				return err
			}
		case LogLevelFirecrackerInfo:
			if err := h.setFirecrackerLogLevel("Info"); err != nil {
				return err
			}
		case LogLevelFirecrackerWarning:
			if err := h.setFirecrackerLogLevel("Warning"); err != nil {
				return err
			}
		case LogLevelFirecrackerOutput:
			h.logFCOutput = true
		case LogLevelFirecrackerSDKDebug:
			if err := h.setFirecrackerSDKLogLevel(logrus.DebugLevel); err != nil {
				return err
			}
		case LogLevelFirecrackerSDKError:
			if err := h.setFirecrackerSDKLogLevel(logrus.ErrorLevel); err != nil {
				return err
			}
		case LogLevelFirecrackerSDKInfo:
			if err := h.setFirecrackerSDKLogLevel(logrus.InfoLevel); err != nil {
				return err
			}
		case LogLevelFirecrackerSDKWarning:
			if err := h.setFirecrackerSDKLogLevel(logrus.WarnLevel); err != nil {
				return err
			}
		case LogLevelFirecrackerContainerdDebug:
			if err := h.setFirecrackerContainerdLogLevel(logrus.DebugLevel); err != nil {
				return err
			}
		case LogLevelFirecrackerContainerdError:
			if err := h.setFirecrackerContainerdLogLevel(logrus.ErrorLevel); err != nil {
				return err
			}
		case LogLevelFirecrackerContainerdInfo:
			if err := h.setFirecrackerContainerdLogLevel(logrus.InfoLevel); err != nil {
				return err
			}
		case LogLevelFirecrackerContainerdWarning:
			if err := h.setFirecrackerContainerdLogLevel(logrus.WarnLevel); err != nil {
				return err
			}
		default:
			return NewInvalidLogLevelError(cleanedLevel)
		}
	}

	return nil
}
