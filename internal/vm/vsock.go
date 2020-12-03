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
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	vsockRetryTimeout      = 20 * time.Second
	vsockRetryInterval     = 100 * time.Millisecond
	unixDialTimeout        = 100 * time.Millisecond
	vsockConnectMsgTimeout = 100 * time.Millisecond
	vsockAckMsgTimeout     = 1 * time.Second
)

// VSockDial attempts to connect to the Firecracker host-side vsock at the provided unix
// path and port. It will retry connect attempts if a temporary error is encountered (up
// to a fixed timeout) or the provided request is canceled.
func VSockDial(ctx context.Context, logger *logrus.Entry, udsPath string, port uint32) (net.Conn, error) {
	tickerCh := time.NewTicker(vsockRetryInterval).C
	var attemptCount int
	for {
		attemptCount++
		logger := logger.WithField("attempt", attemptCount)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tickerCh:
			conn, err := tryConnect(logger, udsPath, port)
			if isTemporaryNetErr(err) {
				err = errors.Wrap(err, "temporary vsock dial failure")
				logger.WithError(err).Debug()
				continue
			} else if err != nil {
				err = errors.Wrap(err, "non-temporary vsock dial failure")
				logger.WithError(err).Error()
				return nil, err
			}

			return conn, nil
		}
	}
}

type vsockListener struct {
	listener net.Listener
	port     uint32
	ctx      context.Context
	logger   *logrus.Entry
}

// VSockListener returns a net.Listener implementation for guest-side Firecracker vsock
// connections.
func VSockListener(ctx context.Context, logger *logrus.Entry, port uint32) (net.Listener, error) {
	listener, err := vsock.Listen(port)
	if err != nil {
		return nil, err
	}

	return vsockListener{
		listener: listener,
		port:     port,
		ctx:      ctx,
		logger:   logger,
	}, nil
}

func (l vsockListener) Accept() (net.Conn, error) {
	ctx, cancel := context.WithTimeout(l.ctx, vsockRetryTimeout)
	defer cancel()

	var attemptCount int
	tickerCh := time.NewTicker(vsockRetryInterval).C
	for {
		attemptCount++
		logger := l.logger.WithField("attempt", attemptCount)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tickerCh:
			conn, err := tryAccept(logger, l.listener, l.port)
			if isTemporaryNetErr(err) {
				err = errors.Wrap(err, "temporary vsock accept failure")
				logger.WithError(err).Debug()
				continue
			} else if err != nil {
				err = errors.Wrap(err, "non-temporary vsock accept failure")
				logger.WithError(err).Error()
				return nil, err
			}

			return conn, nil
		}
	}
}

func (l vsockListener) Close() error {
	return l.listener.Close()
}

func (l vsockListener) Addr() net.Addr {
	return l.listener.Addr()
}

// VSockDialConnector returns an IOConnector for establishing vsock connections
// that are dialed from the host to a guest listener.
func VSockDialConnector(timeout time.Duration, udsPath string, port uint32) IOConnector {
	return func(procCtx context.Context, logger *logrus.Entry) <-chan IOConnectorResult {
		returnCh := make(chan IOConnectorResult)

		go func() {
			defer close(returnCh)
			timeoutCtx, cancel := context.WithTimeout(procCtx, timeout)
			defer cancel()

			conn, err := VSockDial(timeoutCtx, logger, udsPath, port)
			returnCh <- IOConnectorResult{
				ReadWriteCloser: conn,
				Err:             err,
			}
		}()

		return returnCh
	}
}

// VSockAcceptConnector provides an IOConnector that establishes the connection by listening
// on the provided guest-side vsock port and accepting the first connection that comes in.
func VSockAcceptConnector(port uint32) IOConnector {
	return func(procCtx context.Context, logger *logrus.Entry) <-chan IOConnectorResult {
		returnCh := make(chan IOConnectorResult)

		listener, err := VSockListener(procCtx, logger, port)
		if err != nil {
			returnCh <- IOConnectorResult{
				Err: err,
			}
			close(returnCh)
			return returnCh
		}

		go func() {
			defer close(returnCh)
			defer listener.Close()

			conn, err := listener.Accept()
			returnCh <- IOConnectorResult{
				ReadWriteCloser: conn,
				Err:             err,
			}
		}()

		return returnCh
	}
}

func vsockConnectMsg(port uint32) string {
	// The message a host-side connection must write after connecting to a firecracker
	// vsock unix socket in order to establish a connection with a guest-side listener
	// at the provided port number. This is specified in Firecracker documentation:
	// https://github.com/firecracker-microvm/firecracker/blob/master/docs/vsock.md#host-initiated-connections
	return fmt.Sprintf("CONNECT %d\n", port)
}

// tryConnect attempts to dial a guest vsock listener at the provided host-side
// unix socket and provided guest-listener port.
func tryConnect(logger *logrus.Entry, udsPath string, port uint32) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", udsPath, unixDialTimeout)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			closeErr := conn.Close()
			if closeErr != nil {
				logger.WithError(closeErr).Error(
					"failed to close vsock socket after previous error")
			}
		}
	}()

	err = tryConnWrite(conn, vsockConnectMsg(port), vsockConnectMsgTimeout)
	if err != nil {
		return nil, vsockConnectMsgError{cause: err}
	}

	line, err := tryConnReadUntil(conn, '\n', vsockAckMsgTimeout)
	if err != nil {
		return nil, vsockAckError{cause: err}
	}

	// The line would be "OK <assigned_hostside_port>\n", but we don't use the hostside port here.
	// https://github.com/firecracker-microvm/firecracker/blob/master/docs/vsock.md#host-initiated-connections
	if !strings.HasPrefix(line, "OK ") {
		return nil, vsockAckError{
			cause: errors.Errorf(`expected to read "OK <port>", but instead read %q`, line),
		}
	}
	return conn, nil
}

// tryAccept attempts to accept a single host-side connection from the provided
// guest-side listener at the provided port.
func tryAccept(logger *logrus.Entry, listener net.Listener, port uint32) (net.Conn, error) {
	conn, err := listener.Accept()
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			closeErr := conn.Close()
			if closeErr != nil {
				logger.WithError(closeErr).Error(
					"failed to close vsock after previous error")
			}
		}
	}()

	return conn, nil
}

// tryConnReadUntil will try to do a read from the provided conn until the specified
// end character is encounteed. Returning an error if the read does not complete
// within the provided timeout. It will reset socket deadlines to none after returning.
// It's only intended to be used for connect/ack messages, not general purpose reads
// after the vsock connection is established fully.
func tryConnReadUntil(conn net.Conn, end byte, timeout time.Duration) (string, error) {
	conn.SetDeadline(time.Now().Add(timeout))
	defer conn.SetDeadline(time.Time{})

	return bufio.NewReaderSize(conn, 32).ReadString(end)
}

// tryConnWrite will try to do a write to the provided conn, returning an error if
// the write fails, is partial or does not complete within the provided timeout. It
// will reset socket deadlines to none after returning. It's only intended to be
// used for connect/ack messages, not general purpose writes after the vsock
// connection is established fully.
func tryConnWrite(conn net.Conn, expectedWrite string, timeout time.Duration) error {
	conn.SetDeadline(time.Now().Add(timeout))
	defer conn.SetDeadline(time.Time{})

	bytesWritten, err := conn.Write([]byte(expectedWrite))
	if err != nil {
		return err
	}
	if bytesWritten != len(expectedWrite) {
		return errors.Errorf("incomplete write, expected %d bytes but wrote %d",
			len(expectedWrite), bytesWritten)
	}

	return nil
}

type vsockConnectMsgError struct {
	cause error
}

func (e vsockConnectMsgError) Error() string {
	return errors.Wrap(e.cause, "vsock connect message failure").Error()
}

func (e vsockConnectMsgError) Temporary() bool {
	return false
}

type vsockAckError struct {
	cause error
}

func (e vsockAckError) Error() string {
	return errors.Wrap(e.cause, "vsock ack message failure").Error()
}

func (e vsockAckError) Temporary() bool {
	return true
}

// isTemporaryNetErr returns whether the provided error is a retriable
// error, according to the interface defined here:
// https://golang.org/pkg/net/#Error
func isTemporaryNetErr(err error) bool {
	terr, ok := err.(interface {
		Temporary() bool
	})

	return err != nil && ok && terr.Temporary()
}
