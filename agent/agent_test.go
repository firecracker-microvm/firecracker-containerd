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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTaskExecID(t *testing.T) {
	results := make(map[string]bool)

	testcases := []struct {
		taskID string
		execID string
	}{
		{
			taskID: "task-task",
			execID: "task",
		},
		{
			taskID: "task",
			execID: "task-task",
		},
		{
			taskID: "task",
			execID: "task",
		},
		{
			taskID: "task",
			execID: "",
		},
	}
	for _, testcase := range testcases {
		id, err := TaskExecID(testcase.taskID, testcase.execID)
		assert.NoError(t, err, "unexpected error", "valid task/exec ID %+v", testcase)
		if results[id] {
			assert.Fail(t, "unexpected duplicated result", "testcase %+v has duplicated result %q", testcase, id)
		}
		results[id] = true
	}
}

func TestTaskExecIDFails(t *testing.T) {
	testcases := []struct {
		taskID string
		execID string
	}{
		{
			taskID: "ta/sk",
			execID: "",
		},
		{
			taskID: "ta/sk",
			execID: "exec",
		},
		{
			taskID: "task",
			execID: "ex/ec",
		},
		{
			taskID: "ta/sk",
			execID: "ex/ec",
		},
		{
			taskID: "task/",
			execID: "exec/",
		},
		{
			taskID: "/task",
			execID: "/exec",
		},
	}
	for _, testcase := range testcases {
		_, err := TaskExecID(testcase.taskID, testcase.execID)
		assert.Error(t, err, "unexpected nil error", "invalid task/exec ID %+v", testcase)
	}
}
