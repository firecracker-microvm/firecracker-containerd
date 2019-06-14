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
	"time"

	"github.com/mdlayher/vsock"
	"github.com/sirupsen/logrus"
)

// VSockDial attempts to connect to a vsock listener at the provided cid and port with a hardcoded number
// of retries.
func VSockDial(reqCtx context.Context, logger *logrus.Entry, contextID, port uint32) (net.Conn, error) {
	// VM should start within 200ms, vsock dial will make retries at 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, 6.4s
	const (
		retryCount      = 7
		initialDelay    = 100 * time.Millisecond
		delayMultiplier = 2
	)

	var lastErr error
	var currentDelay = initialDelay

	for i := 1; i <= retryCount; i++ {
		select {
		case <-reqCtx.Done():
			return nil, reqCtx.Err()
		default:
			conn, err := vsock.Dial(contextID, port)
			if err == nil {
				logger.WithField("connection", conn).Debug("Dial succeeded")
				return conn, nil
			}

			logger.WithError(err).Warnf("vsock dial failed (attempt %d of %d), will retry in %s", i, retryCount, currentDelay)
			time.Sleep(currentDelay)

			lastErr = err
			currentDelay *= delayMultiplier
		}
	}

	logger.WithError(lastErr).WithFields(logrus.Fields{"context_id": contextID, "port": port}).Error("vsock dial failed")
	return nil, lastErr
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

// VSockAcceptConnector provides an IOConnector that establishes the connection by listening on the provided
// vsock port and accepting the first connection that comes in.
func VSockAcceptConnector(port uint32) IOConnector {
	return func(procCtx context.Context, logger *logrus.Entry) <-chan IOConnectorResult {
		returnCh := make(chan IOConnectorResult)

		go func() {
			defer close(returnCh)

			listener, err := vsock.Listen(port)
			if err != nil {
				returnCh <- IOConnectorResult{
					Err: err,
				}
				return
			}

			defer listener.Close()

			for range time.NewTicker(10 * time.Millisecond).C {
				select {
				case <-procCtx.Done():
					returnCh <- IOConnectorResult{
						Err: procCtx.Err(),
					}
					return
				default:
					// accept is non-blocking so try to accept until we get a connection
					conn, err := listener.Accept()
					if err == nil {
						returnCh <- IOConnectorResult{
							ReadWriteCloser: conn,
						}
						return
					}

					if isTemporaryNetErr(err) {
						logger.WithError(err).Debug("temporary stdio vsock accept failure")
						continue
					}

					logger.WithError(err).Error("non-temporary stdio vsock accept failure")
					returnCh <- IOConnectorResult{
						Err: err,
					}
					return
				}
			}

			panic("unreachable code") // appeases the compiler, which doesn't know the for loop is infinite
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
