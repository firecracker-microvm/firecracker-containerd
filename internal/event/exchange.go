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

package event

import (
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/runtime/v2/shim"
)

var _ shim.Publisher = &ExchangeCloser{}

// ExchangeCloser wraps an event exchange with an extra no-op Close method
// to make it compatible with the containerd shim.Publisher interface
type ExchangeCloser struct {
	*exchange.Exchange
}

// Close is a no-op, just needed to satisfy shim.Publisher interface
func (*ExchangeCloser) Close() error {
	return nil
}
