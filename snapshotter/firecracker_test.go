package snapshotter

import (
	"context"
	_ "crypto/sha256"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/testsuite"
)

func TestCreateImage(t *testing.T) {
	snap := Snapshotter{}

	tempDir, err := ioutil.TempDir("", "fc-snapshotter")
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(tempDir)

	imgPath := filepath.Join(tempDir, "x.img")

	const (
		sizeMB    = 100
		sizeBytes = sizeMB * 100000
	)

	err = snap.createImage(context.Background(), imgPath, sizeMB)
	if err != nil {
		t.Fatal(err)
	}

	if stat, err := os.Stat(imgPath); os.IsNotExist(err) {
		t.Fatal("error creating image file")
	} else if stat.Size() != sizeBytes {
		t.Errorf("wrong image size %d != %d", stat.Size(), sizeBytes)
	}
}

func TestMountUnmount(t *testing.T) {
	snap := Snapshotter{}

	tempDir, err := ioutil.TempDir("", "fc-snapshotter")
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(tempDir)

	imgPath := filepath.Join(tempDir, "x.img")
	mntPath := filepath.Join(tempDir, "/mnt")

	err = snap.createImage(context.Background(), imgPath, 100)
	if err != nil {
		t.Fatal(err)
	}

	if err := snap.mount(imgPath, mntPath, false); err != nil {
		t.Fatal(err)
	}

	if err := snap.unmount(context.Background(), mntPath); err != nil {
		t.Fatal(err)
	}
}

func createSnapshotter(ctx context.Context, root string) (snapshots.Snapshotter, func() error, error) {
	snap, err := NewSnapshotter(ctx, root)
	if err != nil {
		return nil, nil, err
	}

	return snap, func() error { return snap.Close() }, nil
}

func TestSnapshotterSuite(t *testing.T) {
	testsuite.SnapshotterSuite(t, "Snapshotter", createSnapshotter)
}
