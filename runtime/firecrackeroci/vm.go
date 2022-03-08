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

/*
   Copyright The containerd Authors.
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at
       http://www.apache.org/licenses/LICENSE-2.0
   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package firecrackeroci

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/oci"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-spec/specs-go"
)

var (
	defaultUnixEnv = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
)

// WithVMLocalImageConfig configures a spec with the content of the image's config.
// It is similar to containerd's oci.WithImageConfig except that it does not access
// the image's rootfs on the host. Instead, it configures what it can on the host and
// passes information to the agent running in the VM to inspect the image's rootfs.
func WithVMLocalImageConfig(image containerd.Image) oci.SpecOpts {
	return func(ctx context.Context, client oci.Client, container *containers.Container, spec *oci.Spec) error {
		ic, err := image.Config(ctx)
		if err != nil {
			return err
		}
		var (
			ociimage v1.Image
			config   v1.ImageConfig
		)
		switch ic.MediaType {
		case v1.MediaTypeImageConfig, images.MediaTypeDockerSchema2Config:
			p, err := content.ReadBlob(ctx, image.ContentStore(), ic)
			if err != nil {
				return err
			}

			if err := json.Unmarshal(p, &ociimage); err != nil {
				return err
			}
			config = ociimage.Config
		default:
			return fmt.Errorf("unknown image config media type %s", ic.MediaType)
		}
		if spec.Process == nil {
			spec.Process = &specs.Process{}
		}

		defaults := config.Env
		if len(defaults) == 0 {
			defaults = defaultUnixEnv
		}
		spec.Process.Env = replaceOrAppendEnvValues(defaults, spec.Process.Env)
		cmd := config.Cmd
		spec.Process.Args = append([]string{}, config.Entrypoint...)
		spec.Process.Args = append(spec.Process.Args, cmd...)

		cwd := config.WorkingDir
		if cwd == "" {
			cwd = "/"
		}
		spec.Process.Cwd = cwd
		if config.User != "" {
			WithVMLocalUser(config.User)(ctx, client, container, spec)
		}
		return nil
	}
}

// WithVMLocalUser configures the user of the spec.
// It is similar to oci.WithUser except that it doesn't map
// username -> uid or group name -> gid. It passes the user to the
// agent running inside the VM to do that mapping.
func WithVMLocalUser(user string) oci.SpecOpts {
	return func(ctx context.Context, client oci.Client, container *containers.Container, spec *oci.Spec) error {
		// This is technically an LCOW specific field, but we piggy back
		// to get the string user into the VM where will will do the uid/gid mapping
		spec.Process.User.Username = user
		return nil
	}
}

// replaceOrAppendEnvValues returns the defaults with the overrides either
// replaced by env key or appended to the list
func replaceOrAppendEnvValues(defaults, overrides []string) []string {
	cache := make(map[string]int, len(defaults))
	results := make([]string, 0, len(defaults))
	for i, e := range defaults {
		parts := strings.SplitN(e, "=", 2)
		results = append(results, e)
		cache[parts[0]] = i
	}

	for _, value := range overrides {
		// Values w/o = means they want this env to be removed/unset.
		if !strings.Contains(value, "=") {
			if i, exists := cache[value]; exists {
				results[i] = "" // Used to indicate it should be removed
			}
			continue
		}

		// Just do a normal set/update
		parts := strings.SplitN(value, "=", 2)
		if i, exists := cache[parts[0]]; exists {
			results[i] = value
		} else {
			results = append(results, value)
		}
	}

	// Now remove all entries that we want to "unset"
	for i := 0; i < len(results); i++ {
		if results[i] == "" {
			results = append(results[:i], results[i+1:]...)
			i--
		}
	}

	return results
}
