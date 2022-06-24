# Remote Snapshotter Example

This example shows how to build a firecracker-containerd client that can launch lazily-loaded estargz images in a firecracker microvm. If you haven't already, please follow the [remote snapshotter getting started guide.](https://github.com/firecracker-microvm/firecracker-containerd/blob/main/docs/remote-snapshotter-getting-started.md)

# Building

The example can be built with

```
make remote-snapshotter
```

# Usage
To use the example, run (e.g.)

```
./remote-snapshotter ghcr.io/firecracker-microvm/firecracker-containerd/amazonlinux:latest-esgz
```

A list of additional, pre-converted images for testing can be found in the [stargz-snapshotter documentation](https://github.com/containerd/stargz-snapshotter/blob/main/docs/pre-converted-images.md)


This will launch the container inside a microVM with lazy loading supplied by the stargz-snaphotter and connect stdio to the container.

Note: stargz-snapshotter has a fallback mechanism to use an embedded overlay snapshotter if an image is not in estargz format. This will not work with firecracker-containerd because the client is responsible for writing content into the snapshot, but the snapshot mountpoints are isolated inside the VM. 

# Credentials

By default, the example tool will ask for a docker username/password each time it is invoked. It can also read these from environment the variables `$DOCKER_USERNAME` and `$DOCKER_PASSWORD`. 

Note that this means it does not support multiple container registries at the same time, nor credential helpers for retrieving credentials.