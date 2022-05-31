# snapshotter

## Building an esgz Formatted Image

The demux snapshotter uses a publicly available [eStargz](https://github.com/containerd/stargz-snapshotter/blob/main/docs/estargz.md) formatted image in its integration testing pipeline. 
This image has the reference `ghcr.io/firecracker-microvm/firecracker-containerd/amazonlinux:latest-esgz`. 

### Optimize an Amazon Linux Container Image for esgz

The `ctr-remote` binary is needed to optimize an image for the esgz image format. Building the stargz-snapshotter submodule provides `ctr-remote`, a `ctr` wrapper, in `_submodules/stargz-snapshotter/out/ctr-remote`.
Refer to the [containerd stargz-snapshotter documentation on `ctr-remote`](https://github.com/containerd/stargz-snapshotter/blob/main/docs/ctr-remote.md) for additional details.

The root level Makefile provides `make esgz-test-image` for pulling the latest Amazon Linux image locally and optimizing it for eStargz using `ctr-remote` into an image `ghcr.io/firecracker-microvm/firecracker-containerd/amazonlinux:latest-esgz`. 
Code owners of `firecracker-containerd` then push the eStargz optimized image as this publicly available reference using `make push-esgz-test-image` with proper credential environment variables `GH_USER` and `GH_PERSONAL_ACCESS_TOKEN` set.

### Example optimization and push to GHCR

Using default base and esgz images:

```bash
$ make esgz-test-image
$ GH_USER=xxx
# set GH_PERSONAL_ACCESS_TOKEN with command substitution such that it does not show in shell history
$ make \
    GH_USER=$GH_USER \
    GH_PERSONAL_ACCESS_TOKEN=`cat`
    push-esgz-test-image
# enter personal access token and CTRL^D
```

To pull, convert, and push to a repo where the user has permissions with custom base and esgz images:

```bash
$ DEFAULT_BASE_IMAGE=public.ecr.aws/amazonlinux/amazonlinux:2022
$ DEFAULT_ESGZ_IMAGE=ghcr.io/$DESTINATION_REPO/amazonlinux:2022-esgz
$ make \
    DEFAULT_BASE_IMAGE=$DEFAULT_BASE_IMAGE \
    DEFAULT_ESGZ_IMAGE=$DEFAULT_ESGZ_IMAGE \
    esgz-test-image
$ GH_USER=xxx
# set GH_PERSONAL_ACCESS_TOKEN with command substitution such that it does not show in shell history
$ make \
    GH_USER=$GH_USER \
    GH_PERSONAL_ACCESS_TOKEN=`cat`
    DEFAULT_ESGZ_IMAGE=$DEFAULT_ESGZ_IMAGE \
    push-esgz-test-image
# enter personal access token and CTRL^D
```

