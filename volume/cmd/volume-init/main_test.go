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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/firecracker-microvm/firecracker-containerd/volume"
	"github.com/stretchr/testify/require"
)

func mkdirAndTouch(path, content string) error {
	err := os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		return err
	}

	return os.WriteFile(path, []byte(content), 0600)
}

func TestCopy(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()

	err := mkdirAndTouch(filepath.Join(from, "/etc/foobar/hello"), "helloworld")
	require.NoError(t, err)

	err = copy(volume.GuestVolumeImageInput{
		From:    from,
		To:      to,
		Volumes: []string{"/etc/foobar"},
	})
	require.NoError(t, err)

	b, err := os.ReadFile(filepath.Join(to, "/etc/foobar/hello"))
	require.NoError(t, err)
	require.Equal(t, "helloworld", string(b))
}
