// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package losetup

import (
	"os/exec"
	"strings"

	"github.com/pkg/errors"
)

// FindAssociatedLoopDevices returns a list of loop devices attached to a given image
func FindAssociatedLoopDevices(imagePath string) ([]string, error) {
	output, err := losetup("--list", "--output", "NAME", "--noheadings", "--associated", imagePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get loop devices: '%s'", output)
	}

	if output == "" {
		return []string{}, nil
	}

	return strings.Split(output, "\n"), nil
}

// AttachLoopDevice finds first available loop device and associates it with an image.
func AttachLoopDevice(imagePath string) (string, error) {
	return losetup("--find", "--show", imagePath)
}

// DetachLoopDevice detaches loop devices
func DetachLoopDevice(loopDevice ...string) error {
	_, err := losetup("--detach", strings.Join(loopDevice, " "))
	return err
}

// RemoveLoopDevicesAssociatedWithImage detaches all loop devices attached to a given sparse image
func RemoveLoopDevicesAssociatedWithImage(imagePath string) error {
	loopDevices, err := FindAssociatedLoopDevices(imagePath)
	if err != nil {
		return err
	}

	for _, loopDevice := range loopDevices {
		if err = DetachLoopDevice(loopDevice); err != nil {
			return err
		}
	}

	return nil
}

// losetup is a wrapper around losetup command line tool
func losetup(args ...string) (string, error) {
	data, err := exec.Command("losetup", args...).CombinedOutput()
	output := string(data)
	if err != nil {
		return "", errors.Wrapf(err, "losetup %s\nerror: %s\n", strings.Join(args, " "), output)
	}

	return strings.TrimSuffix(output, "\n"), err
}
