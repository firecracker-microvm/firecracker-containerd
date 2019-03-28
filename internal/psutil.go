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

package internal

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/process"
)

// WaitForProcessToExist queries running processes every `queryInterval` duration, blocking until the provided matcher
// function returns true for at least one running process or the context is canceled. If a query returns multiple
// matching processes, all will be returned.
func WaitForProcessToExist(
	ctx context.Context,
	queryInterval time.Duration,
	matcher func(context.Context, *process.Process) (bool, error),
) ([]*process.Process, error) {
	for range time.NewTicker(queryInterval).C {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			processes, err := process.ProcessesWithContext(ctx)
			if err != nil {
				return nil, err
			}

			var matches []*process.Process
			for _, p := range processes {
				isMatch, err := matcher(ctx, p)
				if err != nil {
					return nil, err
				}
				if isMatch {
					matches = append(matches, p)
				}
			}

			if len(matches) > 0 {
				return matches, nil
			}
		}
	}

	panic("unreachable code in WaitForProcesstoExist")
}

// WaitForPidToExit queries running processes every `queryInterval` duration, blocking until the provided pid is found
// to not exist or the context is canceled.
func WaitForPidToExit(ctx context.Context, queryInterval time.Duration, pid int32) error {
	for range time.NewTicker(queryInterval).C {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			pidExists, err := process.PidExistsWithContext(ctx, pid)
			if err != nil {
				return err
			}

			if !pidExists {
				return nil
			}
		}
	}

	panic("unreachable code in WaitForPidToExit")
}
