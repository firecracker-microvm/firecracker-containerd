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

// Copied from https://github.com/containerd/stargz-snapshotter/blob/v0.11.4/fs/config/config.go
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

/*
   Copyright 2019 The Go Authors. All rights reserved.
   Use of this source code is governed by a BSD-style
   license that can be found in the NOTICE.md file.
*/

// Package config provides constants for stargz snapshotter integration tests.
package config

const (
	// TargetSkipVerifyLabel is a snapshot label key that indicates to skip content
	// verification for the layer.
	TargetSkipVerifyLabel = "containerd.io/snapshot/remote/stargz.skipverify"

	// TargetPrefetchSizeLabel is a snapshot label key that indicates size to prefetch
	// the layer. If the layer is eStargz and contains prefetch landmarks, these config
	// will be respeced.
	TargetPrefetchSizeLabel = "containerd.io/snapshot/remote/stargz.prefetch"
)
