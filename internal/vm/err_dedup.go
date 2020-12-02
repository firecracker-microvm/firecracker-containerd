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

package vm

import (
	"github.com/sirupsen/logrus"
)

// errDedup will dedup errors for more condensed logging. Deduplication is
// handled by the string that is returned from the error
type errDedup struct {
	errsCount map[string]int
	errs      []error
	logger    *logrus.Entry
}

func newErrDedup(entry *logrus.Entry) *errDedup {

	return &errDedup{
		errsCount: map[string]int{},
		logger:    entry,
	}
}

// Add adds the given error to the error deduplication struct
func (e *errDedup) Add(err error) {
	errMsg := err.Error()
	e.errsCount[errMsg]++
	if v := e.errsCount[errMsg]; v != 1 {
		return
	}

	// log the first instance of the error
	e.logger.WithError(err).Debug()
	e.errs = append(e.errs, err)
}

// Log will log any error that occurred more than once
func (e errDedup) Log() {
	for _, err := range e.errs {
		count := e.errsCount[err.Error()]
		// since we log the first instance, we will only log errors that occurred
		// more than once
		if count != 1 {
			e.logger.WithError(err).Debugf("occurred %d times", count)
		}
	}
}
