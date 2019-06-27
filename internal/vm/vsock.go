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
	"net"
	"strings"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/sirupsen/logrus"
)

const (
	vsockConnectTimeout = 20 * time.Second
)

// VSockDial attempts to connect to a vsock listener at the provided cid and port with a hardcoded number
// of retries.
func VSockDial(reqCtx context.Context, logger *logrus.Entry, contextID, port uint32) (net.Conn, error) {
	// Retries occur every 100ms up to vsockConnectTimeout
	const retryInterval = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(reqCtx, vsockConnectTimeout)
	defer cancel()

	var attemptCount int
	for range time.NewTicker(retryInterval).C {
		attemptCount++
		logger = logger.WithField("attempt", attemptCount)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			conn, err := vsock.Dial(contextID, port)
			if err == nil {
				logger.WithField("connection", conn).Debug("vsock dial succeeded")
				return conn, nil
			}

			// ENXIO and ECONNRESET can be returned while the VM+agent are still in the midst of booting
			if isTemporaryNetErr(err) || isENXIO(err) || isECONNRESET(err) {
				logger.WithError(err).Debug("temporary vsock dial failure")
				continue
			}

			logger.WithError(err).Error("non-temporary vsock dial failure")
			return nil, err
		}
	}

	panic("unreachable code") // appeases the compiler, which doesn't know the for loop is infinite
}

// VSockDialConnector provides an IOConnector interface to the VSockDial function.
func VSockDialConnector(contextID, port uint32) IOConnector {
	return func(procCtx context.Context, logger *logrus.Entry) <-chan IOConnectorResult {
		returnCh := make(chan IOConnectorResult)

		go func() {
			defer close(returnCh)

			conn, err := VSockDial(procCtx, logger, contextID, port)
			returnCh <- IOConnectorResult{
				ReadWriteCloser: conn,
				Err:             err,
			}
		}()

		return returnCh
	}
}

func vsockAccept(reqCtx context.Context, logger *logrus.Entry, port uint32) (net.Conn, error) {
	listener, err := vsock.Listen(port)
	if err != nil {
		return nil, err
	}

	defer listener.Close()

	// Retries occur every 10ms up to vsockConnectTimeout
	const retryInterval = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(reqCtx, vsockConnectTimeout)
	defer cancel()

	var attemptCount int
	for range time.NewTicker(retryInterval).C {
		attemptCount++
		logger = logger.WithField("attempt", attemptCount)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// accept is non-blocking so try to accept until we get a connection
			conn, err := listener.Accept()
			if err == nil {
				return conn, nil
			}

			if isTemporaryNetErr(err) {
				logger.WithError(err).Debug("temporary stdio vsock accept failure")
				continue
			}

			logger.WithError(err).Error("non-temporary stdio vsock accept failure")
			return nil, err
		}
	}

	panic("unreachable code") // appeases the compiler, which doesn't know the for loop is infinite
}

// VSockAcceptConnector provides an IOConnector that establishes the connection by listening on the provided
// vsock port and accepting the first connection that comes in.
func VSockAcceptConnector(port uint32) IOConnector {
	return func(procCtx context.Context, logger *logrus.Entry) <-chan IOConnectorResult {
		returnCh := make(chan IOConnectorResult)

		go func() {
			defer close(returnCh)

			conn, err := vsockAccept(procCtx, logger, port)
			returnCh <- IOConnectorResult{
				ReadWriteCloser: conn,
				Err:             err,
			}
		}()

		return returnCh
	}
}

func isTemporaryNetErr(err error) bool {
	terr, ok := err.(interface {
		Temporary() bool
	})

	return err != nil && ok && terr.Temporary()
}

// Unfortunately, as "documented" on various online forums, there's no ideal way to
// test for actual Linux error codes returned by the net library or wrappers
// around that library. The common approach is to fall back on string matching,
// which is done for the functions below

func isENXIO(err error) bool {
	return strings.HasSuffix(err.Error(), "no such device")
}

func isECONNRESET(err error) bool {
	return strings.HasSuffix(err.Error(), "connection reset by peer")
}
