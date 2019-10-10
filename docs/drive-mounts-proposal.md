# Proposal - Drive Mount Support in Firecracker-containerd

# Problem

* Firecracker-containerd users may need to attach extra drives containing filesystems (i.e. those besides the rootfs and the container images) and mount them inside their VM. This can enable, for example, extra persistent storage that's bind mounted into container rootfs dirs.
* However, they will run into some of the [same issues](https://github.com/firecracker-microvm/firecracker-containerd/blob/36b47bf0a988083d3b5b15502ce61d5909b8a798/docs/design-approaches.md#block-devices) that Firecracker-containerd did when implementing support for multiple container rootfs drives, namely that there is no way to deterministically map a drive ID outside the VM to a block device within the VM guest.
* This leaves users with a very complicated problem to solve.
    * One (terrible) option would be for them to add custom code to their VM rootfs that will, on VM boot, somehow figure out which block device is which, being very careful to not interfere with any stub drives that Firecracker-containerd plans on using for containers later.
    * This essentially exposes internal implementation details (stub drives) to users to have to deal with

# Potential Solutions

## Option A) Extend CreateVM API (Preferred) 

This option updates Firecracker-containerd’s CreateVM API to allow specifying filesystem images on the host that will be mounted inside the VM on boot at specified paths.

**It’s crucial to note that this is NOT generic “bind-mount-like” support**, which would require something more like virtiofs. This just allows filesystem images available on the host to be mounted within the VM and doesn’t do anything to prevent concurrent access or make concurrent access safe (that’s up to the user).

### User Interface

The API update will look like:

```
message CreateVMRequest {
    // existing fields...

    // Replace "FirecrackerDrive RootDrive" with
    FirecrackerRootDrive RootDrive;

    // Replace "repeated FirecrackerDrive AdditionalDrives" with
    repeated FirecrackerDriveMount DriveMounts;
}

message FirecrackerRootDrive {
    // (Required) HostPath is the path on the host to the filesystem image or device
    // that will supply the rootfs of the VM.
    string HostPath;

    // (Optional) If the HostPath points to a drive or image with multiple
    // partitions, Partuuid specifies which partition will be used to boot
    // the VM
    string Partuuid;

    // (Optional) If set to true, IsReadOnly results in the specified HostPath
    // being opened as read-only by the Firecracker VMM.
    bool IsReadOnly;

    // (Optional) RateLimiter configuration that will be applied to the
    // backing-drive for the VM's rootfs
    FirecrackerRateLimiter RateLimiter;
}

message FirecrackerDriveMount {
    // (Required) HostPath is the path on the host to the filesystem image or device
    // that will be mounted inside the VM.
    string HostPath;

    // (Required) VMPath is the path inside the VM guest at which the filesystem
    // image or device will be mounted.
    string VMPath;

    // (Required) FilesystemType is the filesystem type (i.e. ext4, xfs, etc.), as
    // used when mounting the filesystem image inside the VM. The VM guest kernel
    // is expected to have support for this filesystem.
    string FilesystemType;
    
    // (Optional) Options are fstab-style options that the mount will be performed
    // within the VM (i.e. ["ro", "noatime"]). Defaults to none if not specified. 
    // If "ro" is specified, the specified HostPath will be also opened as read-only
    // by the Firecracker VMM.
    repeated string Options;

    // (Optional) RateLimiter that will be applied to the backing-drive.
    FirecrackerRateLimiter RateLimiter;
}
```

When `DriveMounts` are specified as part of a `CreateVM` request, Firecracker-containerd will internally take care of loading the filesystem image on the host into a Firecracker drive and then mounting the filesystem on that drive inside the VM guest before the `CreateVM` call returns (i.e. as part of the VM+Agent boot process).

The API update is backwards incompatible, but in such a way that upgrading to the new schema is straightforward. It results in a cleaner interface than trying to adapt the previous schema due to:

1. Removing the need to either support or reject rootfs drives that have the new `VMPath`, `FilesystemType` and `Options` field specified
2. Removing the need for a `IsRootDrive` field in our API
3. Removing the need to either support or reject `DriveMount` requests that specify a `Partuuid` 

### Implementation Details

Internally, the implementation can just reuse the current stub drive approach used for container images, which will allow Agent to identify that a given block device is intended to be mounted at a given path and then perform that mount at a later point when the drive has been patched to actually contain the filesystem image. The patching and mounting of the drive would happen entirely internally during a `CreateVM` call, none of that would be exposed to users.

In terms of validation, we will

* Rely on validation that takes place during the mount syscall itself (which prevents, for example, mounts over `/`)
* Reject mounts over critical system directories used by agent+runc, namely
    * `/proc` (used by runc)
    * `/sys` (used by agent)
    * `/dev` (used by agent)
* Specifically **not** try to prevent (that is, allow) any other mounts, including those over `/container`, which is used by agent.
    * There are legitimate use cases for wanting to mount over `/container` (such as wanting to persist container state). 
    * Agent will need to do some extra validation of the contents of `/container` to make sure it’s usable for storing state, but that’s important validation to have irrespective of the new feature being added here.

The validation of not mounting over critical system directories will be best-effort and handle common cases. It will, from within the VM, resolve symlinks of the mount target and check if it’s the banned directory of a subdir of one. Trying to handle more “exotic” cases, such as those involving complicated weaving of bind-mounts and mount-propagation flags that could result in a mount affecting `/proc`, `/sys` or `/dev` despite not being applied to a subdirectory of them, won’t be attempted. Any attempt would most likely be futile and only result in handling outrageously obscure cases. The user is responsible for validation in those cases should they ever arise in practice.

## Option B) Nested Filesystems

Another possibility is to ask users to solve this problem themselves by including any extra needed filesystems as image files within their VM rootfs image (essentially, nested filesystem images). Then, on boot, they can have their VM guest’s init system mount the nested images to known locations. This removes the need for any extra drives to be attached to Firecracker.

However, if the nested filesystem images need to be pre-loaded with different content for different VMs, this means that a new rootfs needs to be generated per VM by the user.

It’s also not immediately clear whether/how this would work with the image-builder tool.
* By default, the upper dir used by the image-builder rootfs is a tmpfs, so that would likely mean that any writes to the nested filesystem images would ultimately go to tmpfs.
* On the other hand, if they want to use non-tmpfs as the upper dir of the image-builder rootfs, then a separate block device needs to be attached, which leads us back to the original problem

# Conclusion

The first option, “Extend CreateVM API”, is preferred because it results in a much simpler interface for users to work with. It also takes advantage of our existing stub drive implementation and thus introduces only minor extra internal complexity to our code.
