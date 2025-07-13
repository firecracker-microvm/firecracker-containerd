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

// Package debug provides utilities for handling multi-level logging.
package debug

import (
	"fmt"
)

// InvalidLogLevelError is an error that will be returned in the event that an
// invalid log level was provided
type InvalidLogLevelError struct {
	logLevel string
}

// NewInvalidLogLevelError will constract an InvalidLogLevelError
func NewInvalidLogLevelError(logLevel string) error {
	return &InvalidLogLevelError{
		logLevel: logLevel,
	}
}

func (e *InvalidLogLevelError) Error() string {
	return fmt.Sprintf("log level %q is an invalid log level", e.logLevel)
}
