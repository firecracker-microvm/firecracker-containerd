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
