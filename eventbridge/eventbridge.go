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

package eventbridge

import (
	"context"

	// We need the typeurl register calls that occur in the init of this package in order to be able to unmarshal events
	// in the Republish func below
	_ "github.com/containerd/containerd/api/events"

	// Even though we are following the v2 runtime model, we are currently re-using a struct definition (Envelope) from
	// the v1 event API
	eventapi "github.com/containerd/containerd/api/services/events/v1"

	"github.com/containerd/containerd/events"
	"github.com/containerd/ttrpc"
	"github.com/containerd/typeurl"
	"github.com/gogo/protobuf/types"
)

const (
	getterServiceName  = "aws.firecracker.containerd.eventbridge.getter"
	getEventMethodName = "GetEvent"
)

// The Getter interface provides an API for retrieving containerd events from a remote event source, such as an
// exchange. It exists separately from containerd's existing EventClient and EventServer interfaces in order to support
// retrieving events via long-polling as opposed to a streaming model. This allows us to use TTRPC for the
// implementation, which currently does not support streaming.
type Getter interface {
	GetEvent(ctx context.Context) (*eventapi.Envelope, error)
}

type getterService struct {
	eventChan <-chan *events.Envelope
	errChan   <-chan error
}

// NewGetterService returns a server-side implementation of the Getter interface. Given an existing event source, it
// will subscribe to that source with no filters. Each GetEvent call pops and returns an event buffered in the
// subscription's channel of published events. If no event is buffered, it blocks until one is.
func NewGetterService(ctx context.Context, eventSource events.Subscriber) Getter {
	eventChan, errChan := eventSource.Subscribe(ctx)
	return &getterService{
		eventChan: eventChan,
		errChan:   errChan,
	}
}

// GetEvent pops and returns an event buffered in the service's subscription channel.
func (s *getterService) GetEvent(ctx context.Context) (*eventapi.Envelope, error) {
	select {
	case receivedEnvelope := <-s.eventChan:
		return &eventapi.Envelope{
			Timestamp: receivedEnvelope.Timestamp,
			Namespace: receivedEnvelope.Namespace,
			Topic:     receivedEnvelope.Topic,
			Event:     receivedEnvelope.Event,
		}, nil
	case err := <-s.errChan:
		return nil, err
	case <-ctx.Done():
		if ctxErr := ctx.Err(); ctxErr != context.Canceled {
			return nil, ctxErr
		}
		return nil, nil
	}
}

// RegisterGetterService adds the Getter service as a method to the provided TTRPC server.
func RegisterGetterService(srv *ttrpc.Server, svc Getter) {
	srv.Register(getterServiceName, map[string]ttrpc.Method{
		getEventMethodName: func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
			return svc.GetEvent(ctx)
		},
	})
}

type getterClient struct {
	rpcClient *ttrpc.Client
}

// NewGetterClient returns a client-side implementation of the Getter interface using the provided TTRPC client to
// connect to the server.
func NewGetterClient(rpcClient *ttrpc.Client) Getter {
	return &getterClient{
		rpcClient: rpcClient,
	}
}

// GetEvent requests and returns an event from the Getter service. If no event is available at the time of the request
// it will block until one is.
func (c *getterClient) GetEvent(ctx context.Context) (*eventapi.Envelope, error) {
	var req types.Empty
	var resp eventapi.Envelope
	if err := c.rpcClient.Call(ctx, getterServiceName, getEventMethodName, &req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Attach takes an existing Getter and forwards each event retrieved from it to the provided forwarder. For example, if
// the source is a client for a remote exchange and sink is a local exchange, Attach will result in each event published
// on the remote exchange to be forwarded to the local exchange, essentially bridging the two together.
func Attach(ctx context.Context, source Getter, sink events.Forwarder) error {
	for {
		// If the context was closed, return. Otherwise, don't get hung up and continue on with the loop.
		select {
		case <-ctx.Done():
			if ctxErr := ctx.Err(); ctxErr != context.Canceled {
				return ctxErr
			}
			return nil
		default:
		}

		envelope, err := source.GetEvent(ctx)
		if err != nil {
			return err
		}

		err = sink.Forward(ctx, &events.Envelope{
			Timestamp: envelope.Timestamp,
			Namespace: envelope.Namespace,
			Topic:     envelope.Topic,
			Event:     envelope.Event,
		})
		if err != nil {
			return err
		}
	}
}

// Republish subscribes to the provided source and publishes each received event on the provided sink. Note that the
// timestamp and namespace of the event will be set via the caller's context due to the use of Publish, as opposed to
// Forward.
func Republish(ctx context.Context, source events.Subscriber, sink events.Publisher) error {
	eventChan, errChan := source.Subscribe(ctx)
	for {
		select {
		case envelope := <-eventChan:
			decodedEvent, err := typeurl.UnmarshalAny(envelope.Event)
			if err != nil {
				return err
			}

			err = sink.Publish(ctx, envelope.Topic, decodedEvent)
			if err != nil {
				return err
			}
		case err := <-errChan:
			return err
		case <-ctx.Done():
			if ctxErr := ctx.Err(); ctxErr != context.Canceled {
				return ctxErr
			}
			return nil
		}
	}
}
