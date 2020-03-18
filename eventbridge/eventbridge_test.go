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

package eventbridge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/containerd/containerd/api/events"
	eventtypes "github.com/containerd/containerd/events"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl"
	"github.com/stretchr/testify/require"
)

const (
	namespace = "tests"
)

func TestEventBridgeAttach(t *testing.T) {
	ctx, cancel := context.WithCancel(namespaces.WithNamespace(context.Background(), namespace))

	source := exchange.NewExchange()
	sink := exchange.NewExchange()
	getter := NewGetterService(ctx, source)

	attachErrCh := Attach(ctx, getter, sink)
	verifyPublishAndReceive(ctx, t, source, sink)

	cancel()
	timeout := time.Second
	select {
	case err := <-attachErrCh:
		if err != context.Canceled {
			require.NoError(t, err, "unexpected Attach error")
		}
	case <-time.After(timeout):
		require.Fail(t, "timeout", "waiting for Attach to return timed out after %s", timeout.String())
	}
}

func TestEventBridgeRepublish(t *testing.T) {
	ctx, cancel := context.WithCancel(namespaces.WithNamespace(context.Background(), namespace))

	source := exchange.NewExchange()
	sink := exchange.NewExchange()

	republishErrCh := Republish(ctx, source, sink)
	verifyPublishAndReceive(ctx, t, source, sink)

	cancel()
	timeout := time.Second
	select {
	case err := <-republishErrCh:
		if err != context.Canceled {
			require.NoError(t, err, "unexpected Republish error")
		}
	case <-time.After(timeout):
		require.Fail(t, "timeout", "waiting for Republish to return timed out after %s", timeout.String())
	}
}

func verifyPublishAndReceive(ctx context.Context, t *testing.T, source eventtypes.Publisher, sink eventtypes.Subscriber) {
	topic := "/just/container/things"
	sinkEventCh, sinkErrorCh := sink.Subscribe(ctx, fmt.Sprintf(`topic=="%s"`, topic))

	for i := 0; i < 100; i++ {
		taskExitEvent := &events.TaskExit{
			ContainerID: fmt.Sprintf("container-%d", i),
			ID:          fmt.Sprintf("id-%d", i),
			Pid:         uint32(i),
			ExitStatus:  uint32(i + 1),
			ExitedAt:    time.Now().UTC(),
		}

		err := source.Publish(ctx, topic, taskExitEvent)
		require.NoError(t, err, "error while publishing to source")

		timeout := time.Second
		select {
		case <-time.After(timeout):
			require.Fail(t, "timeout", "waiting for published message timed out after %s", timeout.String())
		case envelope := <-sinkEventCh:
			require.Equal(t, topic, envelope.Topic, "received expected envelope topic")
			require.Equal(t, namespace, envelope.Namespace, "received expected envelope namespace")
			require.WithinDuration(t, time.Now().UTC(), envelope.Timestamp, timeout, "received expected envelope timestamp")

			receivedEvent, err := typeurl.UnmarshalAny(envelope.Event)
			require.NoError(t, err, "failed to unmarshal event from topic %s", envelope.Topic)

			switch receivedTaskExitEvent := receivedEvent.(type) {
			case *events.TaskExit:
				require.Equal(t, taskExitEvent.ContainerID, receivedTaskExitEvent.ContainerID, "received expected ContainerID")
				require.Equal(t, taskExitEvent.ID, receivedTaskExitEvent.ID, "received expected ID")
				require.Equal(t, taskExitEvent.Pid, receivedTaskExitEvent.Pid, "received expected Pid")
				require.Equal(t, taskExitEvent.ExitStatus, receivedTaskExitEvent.ExitStatus, "received expected ExitStatus")
				require.Equal(t, taskExitEvent.ExitedAt, receivedTaskExitEvent.ExitedAt, "received expected ExitedAt")
			default:
				require.Fail(t, "unexpected event", "received unexpected event type on topic %s", envelope.Topic)
			}
		case err := <-sinkErrorCh:
			require.Fail(t, "unexpected error", "unexpectedly received on sink error chan: %v", err)
		}

		select {
		case err := <-sinkErrorCh:
			require.Fail(t, "unexpected error", "unexpectedly received on sink error chan: %v", err)
		default:
		}
	}
}
