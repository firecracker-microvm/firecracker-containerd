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

package shim

import (
	"context"

	"github.com/containerd/containerd/runtime/v2/shim"
)

// FCControlSocketAddress is a unix socket address at which a shim
// with the given namespace, the containerd socket address, and the vmID will listen and serve
// the fccontrol api
func FCControlSocketAddress(namespacedCtx context.Context, socketPath, vmID string) (string, error) {
	shimSocketAddr, err := shim.SocketAddress(namespacedCtx, socketPath, vmID)
	if err != nil {
		return "", err
	}

	return shimSocketAddr + "-fccontrol", nil
}
