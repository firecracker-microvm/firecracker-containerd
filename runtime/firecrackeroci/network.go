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

package firecrackeroci

import (
	"context"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
)

var _ oci.SpecOpts = WithVMNetwork

// WithVMNetwork modifies a container to use its host network settings (host netns, host utsns, host
// /etc/resolv.conf and host /etc/hosts). It's intended to configure Firecracker-containerd containers
// to have access to the network (if any) their VM was configured with.
func WithVMNetwork(ctx context.Context, cli oci.Client, ctr *containers.Container, spec *oci.Spec) error {
	for _, opt := range []oci.SpecOpts{
		oci.WithHostNamespace(specs.NetworkNamespace),
		oci.WithHostNamespace(specs.UTSNamespace),
		oci.WithHostResolvconf,
		oci.WithHostHostsFile,
	} {
		err := opt(ctx, cli, ctr, spec)
		if err != nil {
			return err
		}
	}

	return nil
}
