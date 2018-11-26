# TODO

## Snapshotter

The existing snapshotter is, by design, simple and broadly compatible
but not very efficient. We should implement an alternative snapshotter
following a model similar to Docker's device-mapper storage driver.

## Networking

Integration of CNI into the appropriate components such that
MicroVM-enclosed containers have network access.

Because Firecracker requires the use of Linux tap devices, many
existing CNI plugins will not work. Design one or more plugins to
facilitate various network configurations.

## Runtime

We are not currently proxying container stdio streams.

## VM and Kernel Images

Our VM image generation tools aren't available yet, nor have we published any
pre-built images.

## Testing

Most components do not currently have any automated tests.
