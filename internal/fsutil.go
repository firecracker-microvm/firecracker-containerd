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

package internal

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

// CreateFSImg will create a file containing a filesystem image of the provided type containing
// the provided files. It returns the path at which the image file can be found.
func CreateFSImg(ctx context.Context, t *testing.T, fsType string, testFiles ...FSImgFile) string {
	t.Helper()

	switch fsType {
	case "ext3", "ext4":
		return createTestExtImg(ctx, t, fsType, testFiles...)
	default:
		require.FailNowf(t, "unsupported fs type %q", fsType)
		return ""
	}
}

// FSImgFile represents a file intended to be place in a filesystem image (at the provided subpath
// with the provided contents)
type FSImgFile struct {
	Subpath  string
	Contents string
}

func createTestExtImg(ctx context.Context, t *testing.T, extName string, testFiles ...FSImgFile) string {
	t.Helper()

	tempdir, err := ioutil.TempDir("", "")
	require.NoError(t, err, "failed to create temp dir for ext img")

	for _, testFile := range testFiles {
		destPath := filepath.Join(tempdir, testFile.Subpath)

		err = os.MkdirAll(filepath.Dir(destPath), 0750)
		require.NoError(t, err, "failed to mkdir for contents of ext img file")

		err = ioutil.WriteFile(destPath, []byte(testFile.Contents), 0750)
		require.NoError(t, err, "failed to write file for contents of ext img")
	}

	imgFile, err := ioutil.TempFile("", "")
	require.NoError(t, err, "failed to obtain temp file for ext img")

	output, err := exec.CommandContext(ctx, "mkfs."+extName, "-d", tempdir, imgFile.Name(), "65536").CombinedOutput()
	require.NoErrorf(t, err, "failed to create ext img, command output:\n %s", string(output))
	return imgFile.Name()
}

// MountInfo holds data parsed from a line of /proc/mounts
type MountInfo struct {
	SourcePath string
	DestPath   string
	Type       string
	Options    []string
}

// ParseProcMountLines converts the provided strings, presumed to be lines read from /proc/mounts
// into MountInfo objects holding the parsed data about each mount.
func ParseProcMountLines(lines ...string) ([]MountInfo, error) {
	mountInfos := []MountInfo{}
	for _, line := range lines {
		if line == "" {
			continue
		}

		// see man 5 fstab for the format of /proc/mounts
		var (
			source     string
			dest       string
			fstype     string
			optionsStr string
			dumpFreq   int
			passno     int
		)
		_, err := fmt.Sscanf(line, "%s %s %s %s %d %d", &source, &dest, &fstype, &optionsStr, &dumpFreq, &passno)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse /proc/mount line %q", line)
		}

		mountInfos = append(mountInfos, MountInfo{
			SourcePath: source,
			DestPath:   dest,
			Type:       fstype,
			Options:    strings.Split(optionsStr, ","),
		})
	}

	return mountInfos, nil
}
