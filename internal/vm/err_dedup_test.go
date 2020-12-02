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
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestErrDedup(t *testing.T) {
	buf := bytes.NewBuffer([]byte{})
	logger := logrus.New()
	logger.Out = buf
	logger.Level = logrus.DebugLevel

	entry := logrus.NewEntry(logger)
	e := newErrDedup(entry)

	errorMsg := "foo"
	expectedSubErrString := fmt.Sprintf("error=%s", errorMsg)
	err := fmt.Errorf(errorMsg)
	e.Add(err)
	e.Log()
	count := strings.Count(buf.String(), expectedSubErrString)
	expectedCount := 1
	assert.Equal(t, expectedCount, count)

	e.Add(err)
	e.Log()
	expectedCount = 2
	count = strings.Count(buf.String(), expectedSubErrString)
	assert.Equal(t, expectedCount, count)

	e.Add(fmt.Errorf("bar"))
	count = strings.Count(buf.String(), expectedSubErrString)
	assert.Equal(t, expectedCount, count)
	expectedCount = 1
	expectedSubErrString = "error=bar"
	count = strings.Count(buf.String(), expectedSubErrString)
	assert.Equal(t, expectedCount, count)
}
