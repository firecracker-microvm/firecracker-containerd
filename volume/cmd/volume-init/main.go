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

// volume-init reads volume mapping configuration from stdin and copies
// the requested directories into the guest volume image.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/containerd/continuity/fs"
	"github.com/firecracker-microvm/firecracker-containerd/volume"
)

func copyVolume(in volume.GuestVolumeImageInput) error {
	for _, v := range in.Volumes {
		to, err := fs.RootPath(in.To, v)
		if err != nil {
			return fmt.Errorf("failed to join %q and %q: %w", in.To, v, err)
		}

		from, err := fs.RootPath(in.From, v)
		if err != nil {
			return fmt.Errorf("failed to join %q and %q: %w", in.From, v, err)
		}

		err = os.MkdirAll(to, 0700)
		if err != nil {
			return err
		}

		err = fs.CopyDir(to, from)
		if err != nil {
			return err
		}
	}
	return nil
}

func realMain() error {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}

	var in volume.GuestVolumeImageInput
	err = json.Unmarshal(b, &in)
	if err != nil {
		return err
	}

	return copyVolume(in)
}

func main() {
	err := realMain()
	if err != nil {
		out := volume.GuestVolumeImageOutput{Error: err.Error()}
		b, err := json.Marshal(out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to unmarshal %+v: %s", out, err)
			os.Exit(2)
		}
		fmt.Printf("%s", string(b))
		os.Exit(1)
	}
}
