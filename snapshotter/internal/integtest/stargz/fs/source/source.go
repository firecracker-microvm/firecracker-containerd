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

// Copied from https://github.com/containerd/stargz-snapshotter/blob/v0.11.4/fs/source/source.go
//
// Taking Stargz as a code dependency required dropping support for Go 1.16.
// Used to add Stargz annotations to pull requests.

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

package source

import (
	"context"
	"fmt"
	"strings"

	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/labels"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/internal/integtest/stargz/fs/config"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	// targetRefLabel is a label which contains image reference.
	targetRefLabel = "containerd.io/snapshot/remote/stargz.reference"

	// targetDigestLabel is a label which contains layer digest.
	targetDigestLabel = "containerd.io/snapshot/remote/stargz.digest"

	// targetImageLayersLabel is a label which contains layer digests contained in
	// the target image.
	targetImageLayersLabel = "containerd.io/snapshot/remote/stargz.layers"

	// targetImageURLsLabelPrefix is a label prefix which constructs a map from the layer index to
	// urls of the layer descriptor.
	targetImageURLsLabelPrefix = "containerd.io/snapshot/remote/urls."

	// targetURsLLabel is a label which contains layer URL. This is only used to pass URL from containerd
	// to snapshotter.
	targetURLsLabel = "containerd.io/snapshot/remote/urls"
)

// AppendDefaultLabelsHandlerWrapper makes a handler which appends image's basic
// information to each layer descriptor as annotations during unpack. These
// annotations will be passed to this remote snapshotter as labels and used to
// construct source information.
func AppendDefaultLabelsHandlerWrapper(ref string, prefetchSize int64) func(f images.Handler) images.Handler {
	return func(f images.Handler) images.Handler {
		return images.HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
			children, err := f.Handle(ctx, desc)
			if err != nil {
				return nil, err
			}
			switch desc.MediaType {
			case ocispec.MediaTypeImageManifest, images.MediaTypeDockerSchema2Manifest:
				for i := range children {
					c := &children[i]
					if images.IsLayerType(c.MediaType) {
						if c.Annotations == nil {
							c.Annotations = make(map[string]string)
						}
						c.Annotations[targetRefLabel] = ref
						c.Annotations[targetDigestLabel] = c.Digest.String()
						var layers string
						for i, l := range children[i:] {
							if images.IsLayerType(l.MediaType) {
								ls := fmt.Sprintf("%s,", l.Digest.String())
								// This avoids the label hits the size limitation.
								// Skipping layers is allowed here and only affects performance.
								if err := labels.Validate(targetImageLayersLabel, layers+ls); err != nil {
									break
								}
								layers += ls

								// Store URLs of the neighbouring layer as well.
								urlsKey := targetImageURLsLabelPrefix + fmt.Sprintf("%d", i)
								c.Annotations[urlsKey] = appendWithValidation(urlsKey, l.URLs)
							}
						}
						c.Annotations[targetImageLayersLabel] = strings.TrimSuffix(layers, ",")
						c.Annotations[config.TargetPrefetchSizeLabel] = fmt.Sprintf("%d", prefetchSize)

						// store URL in annotation to let containerd to pass it to the snapshotter
						c.Annotations[targetURLsLabel] = appendWithValidation(targetURLsLabel, c.URLs)
					}
				}
			}
			return children, nil
		})
	}
}

func appendWithValidation(key string, values []string) string {
	var v string
	for _, u := range values {
		s := fmt.Sprintf("%s,", u)
		if err := labels.Validate(key, v+s); err != nil {
			break
		}
		v += s
	}
	return strings.TrimSuffix(v, ",")
}
