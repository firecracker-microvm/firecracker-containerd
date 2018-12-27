# snapshotter

The snapshotter is an out-of-process gRPC proxy plugin for containerd that
implements containerd's snapshotter API.  This snapshotter creates snapshots as
filesystem images that can be exposed to Firecracker microVMs as devices; this
is necessary as the Firecracker VMM does not enable any filesystem-level sharing
between the microVM and the host.

## Current implementation

We have two current implementations of snapshotters that are device-based and
work with containerd and Firecracker microVMs.

### naive snapshotter

This snapshotter is very simple and naive; it relies on copying data for every
snapshot and does not perform content deduplication.  However, it serves as a
useful proof-of-concept for integration with Firecracker and as a sanity-check
to ensure that the other snapshotter is behaving correctly (i.e., other than
performance, we should see exactly the same behavior).

### devmapper snapshotter

This is a snapshotter that leverages
[thin provisioning](https://www.kernel.org/doc/Documentation/device-mapper/thin-provisioning.txt)
of virtual devices with device-mapper.  Using this snapshotter, we can achieve
efficient, deduplicated storage between content layers and their associated
images or containers.

There is a reasonably long history of using device-mapper in this manner in
Docker, with a variety of implications for performance and stability.  We do
need a device-based snapshotter for Firecracker, and device-mapper offers a
straightforward path to do so.  However, there have been concerns expressed
around file read/write/copy-on-write performance, as well as around provisioning
and deactivation performance.

## Plans

We plan to continue exploring models for device-based, deduplicated snapshot
storage.  Along with the devmapper snapshotter that we've already implemented,
we're interested in approaches combining the
[overlay filesystem](https://www.kernel.org/doc/Documentation/filesystems/overlayfs.txt)
inside the microVM with device-based storage attached from the host.