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

package vm

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// IOProxy is an interface to a particular implementation for initializing
// and copying the stdio of a process running in a VM. All its methods are
// unexported as they are used only internally. The interface exists just
// to give outside callers a choice of implementation when setting up a
// process.
type IOProxy interface {
	// start should begin initialization of stdio proxying for the provided
	// process. It returns two channels, one to indicate io initialization
	// is completed and one to indicate io copying is completed.
	start(proc *vmProc) (ioInitDone <-chan error, ioCopyDone <-chan error)
}

// IOConnector is function that begins initializing an IO connection (i.e.
// vsock, FIFO, etc.). It returns a channel that should be published to with
// an IOConnectorResult object once initialization is complete or an error
// occurs.
//
// The return of a channel instead of plain values gives the implementation
// the freedom to do synchronous setup, asynchronous setup or a mix of both.
//
// The provided context is canceled if the process fails to create/exec or
// when the process exits after a successful create/exec.
type IOConnector func(procCtx context.Context, logger *logrus.Entry) <-chan IOConnectorResult

// IOConnectorResult represents the result of attempting to establish an IO
// connection. If successful, the ReadWriteCloser should be non-nil and Err
// should be nil. If unsuccessful, ReadWriteCloser should be nil and Err
// should be non-nil.
type IOConnectorResult struct {
	io.ReadWriteCloser
	Err error
}

// IOConnectorPair holds the read and write side of IOConnectors whose IO
// should be proxied.
type IOConnectorPair struct {
	ReadConnector  IOConnector
	WriteConnector IOConnector
}

func (connectorPair *IOConnectorPair) proxy(
	proc *vmProc,
	logger *logrus.Entry,
	timeoutAfterExit time.Duration,
) (ioInitDone <-chan error, ioCopyDone <-chan error) {
	initDone := make(chan error, 2)
	copyDone := make(chan error)

	// Start the initialization process. Any synchronous setup made by the connectors will
	// be completed after these lines. Async setup will be done once initDone is closed in
	// the goroutine below.
	readerResultCh := connectorPair.ReadConnector(proc.ctx, logger.WithField("direction", "read"))
	writerResultCh := connectorPair.WriteConnector(proc.ctx, logger.WithField("direction", "write"))

	go func() {
		defer close(copyDone)

		var reader io.ReadCloser
		var writer io.WriteCloser
		var ioInitErr error

		// Send the first error we get to initDone, but consume both so we can ensure both
		// end up closed in the case of an error
		for readerResultCh != nil || writerResultCh != nil {
			var err error
			select {
			case readerResult := <-readerResultCh:
				readerResultCh = nil
				reader, err = readerResult.ReadWriteCloser, readerResult.Err
			case writerResult := <-writerResultCh:
				writerResultCh = nil
				writer, err = writerResult.ReadWriteCloser, writerResult.Err
			}

			if err != nil {
				ioInitErr = errors.Wrap(err, "error initializing io")
				logger.WithError(ioInitErr).Error()
				initDone <- ioInitErr
			}
		}

		close(initDone)
		if ioInitErr != nil {
			logClose(logger, reader, writer)
			return
		}

		// IO streams have been initialized successfully

		// Once the proc exits, wait the provided time before forcibly closing io streams.
		// If the io streams close on their own before the timeout, the Close calls here
		// should just be no-ops.
		go func() {
			<-proc.ctx.Done()
			time.AfterFunc(timeoutAfterExit, func() {
				logClose(logger, reader, writer)
			})
		}()

		logger.Debug("begin copying io")
		defer logger.Debug("end copying io")

		_, err := io.CopyBuffer(writer, reader, make([]byte, internal.DefaultBufferSize))
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") ||
				strings.Contains(err.Error(), "file already closed") {
				logger.Infof("connection was closed: %v", err)
			} else {
				logger.WithError(err).Error("error copying io")
			}
			copyDone <- err
		}
	}()

	return initDone, copyDone
}

type ioConnectorSet struct {
	stdin  *IOConnectorPair
	stdout *IOConnectorPair
	stderr *IOConnectorPair
}

// NewIOConnectorProxy implements the IOProxy interface for a set of
// IOConnectorPairs, one each for stdin, stdout and stderr. If any one of
// those streams does not need to be proxied, the corresponding arg should
// be nil.
func NewIOConnectorProxy(stdin, stdout, stderr *IOConnectorPair) IOProxy {
	return &ioConnectorSet{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
}

func (ioConnectorSet *ioConnectorSet) start(proc *vmProc) (ioInitDone <-chan error, ioCopyDone <-chan error) {
	var initErrG errgroup.Group
	var copyErrG errgroup.Group
	waitErrs := func(initErrCh, copyErrCh <-chan error) {
		initErrG.Go(func() error { return <-initErrCh })
		copyErrG.Go(func() error { return <-copyErrCh })
	}

	if ioConnectorSet.stdin != nil {
		// For Stdin only, provide 0 as the timeout to wait after the proc exits before closing IO streams.
		// There's no reason to send stdin data to a proc that's already dead.
		waitErrs(ioConnectorSet.stdin.proxy(proc, proc.logger.WithField("stream", "stdin"), 0))
	} else {
		proc.logger.Debug("skipping proxy io for unset stdin")
	}

	if ioConnectorSet.stdout != nil {
		waitErrs(ioConnectorSet.stdout.proxy(proc, proc.logger.WithField("stream", "stdout"), defaultIOFlushTimeout))
	} else {
		proc.logger.Debug("skipping proxy io for unset stdout")
	}

	if ioConnectorSet.stderr != nil {
		waitErrs(ioConnectorSet.stderr.proxy(proc, proc.logger.WithField("stream", "stderr"), defaultIOFlushTimeout))
	} else {
		proc.logger.Debug("skipping proxy io for unset stderr")
	}

	initDone := make(chan error)
	go func() {
		defer close(initDone)
		initDone <- initErrG.Wait()
	}()

	copyDone := make(chan error)
	go func() {
		defer close(copyDone)
		copyDone <- copyErrG.Wait()
	}()

	return initDone, copyDone
}

func logClose(logger *logrus.Entry, streams ...io.Closer) {
	var closeErr error
	for _, stream := range streams {
		if stream == nil {
			continue
		}

		err := stream.Close()
		if err != nil {
			closeErr = multierror.Append(closeErr, err)
		}
	}

	if closeErr != nil {
		logger.WithError(closeErr).Error("error closing io stream")
	}
}
