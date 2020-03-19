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

package internal

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/cpu"
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

// AverageCPUDeltas returns the average CPU cycles consumed per sampleInterval, as calculated
// between the time this function is invoked and the time the provided ctx is cancelled.
func AverageCPUDeltas(ctx context.Context, sampleInterval time.Duration) (*cpu.TimesStat, error) {
	sampleCount := 0
	sampleCh := sampleCPUTimes(ctx, sampleInterval)

	firstSample := <-sampleCh
	if firstSample == nil {
		return nil, errors.New("sample channel closed before first data point")
	}
	if firstSample.Err != nil {
		return nil, firstSample.Err
	}

	var lastSample *cpuTimesSample
	for lastSample = range sampleCh {
		sampleCount++
		if lastSample.Err != nil {
			return nil, lastSample.Err
		}
	}

	if lastSample == nil {
		return nil, errors.New("only got one data point, cannot calculate average")
	}

	avg := func(first float64, last float64) float64 {
		return (last - first) / float64(sampleCount)
	}

	return &cpu.TimesStat{
		User:      avg(firstSample.User, lastSample.User),
		System:    avg(firstSample.System, lastSample.System),
		Idle:      avg(firstSample.Idle, lastSample.Idle),
		Nice:      avg(firstSample.Nice, lastSample.Nice),
		Iowait:    avg(firstSample.Iowait, lastSample.Iowait),
		Irq:       avg(firstSample.Irq, lastSample.Irq),
		Softirq:   avg(firstSample.Softirq, lastSample.Softirq),
		Steal:     avg(firstSample.Steal, lastSample.Steal),
		Guest:     avg(firstSample.Guest, lastSample.Guest),
		GuestNice: avg(firstSample.GuestNice, lastSample.GuestNice),
	}, nil
}

type cpuTimesSample struct {
	*cpu.TimesStat
	Index int
	Err   error
}

func sampleCPUTimes(ctx context.Context, sampleInterval time.Duration) <-chan *cpuTimesSample {
	returnCh := make(chan *cpuTimesSample)
	go func() {
		defer close(returnCh)

		var index int
		for range time.NewTicker(sampleInterval).C {
			select {
			case <-ctx.Done():
				return
			default:
				index++
			}

			sample := &cpuTimesSample{
				Index: index,
			}

			const percpu = false // just give us the aggregate numbers, not each cpu's numbers
			curTimesList, err := cpu.Times(percpu)
			if err != nil {
				sample.Err = err
				returnCh <- sample
				return
			}

			// we set percpu to false, so there should be only one stat
			if len(curTimesList) != 1 {
				sample.Err = errors.Errorf("unexpected number of cpu times in sample %d", len(curTimesList))
				returnCh <- sample
				return
			}
			sample.TimesStat = &curTimesList[0]

			select {
			case returnCh <- sample:
			default:
				// just skip the sample if there's no room for it
			}
		}
	}()
	return returnCh
}
