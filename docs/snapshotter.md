# snapshotter

The snapshotter is an out-of-process gRPC proxy plugin for containerd that
implements containerd's snapshotter API.  This snapshotter creates snapshots as
filesystem images that can be exposed to Firecracker microVMs as devices; this
is necessary as the Firecracker VMM does not enable any filesystem-level sharing
between the microVM and the host.

## Current implementation

The current implementation of this snapshotter is very simple and naive; it
relies on copying data for every snapshot and does not perform content
deduplication.  However, it serves as a useful proof-of-concept for integration
with Firecracker.

We plan to replace this implementation with one that does not duplicate content
between layers.