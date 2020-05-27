## Logging Congiguration
--

firecracker-containerd allows for users to specify per application/library
logging. We do this by setting the `log_levels` field in the
firecracker-runtime.json file.

```json
{
  "firecracker_binary_path": "/usr/local/bin/firecracker",
  "kernel_image_path": "/var/lib/firecracker-containerd/runtime/default-vmlinux.bin",
  "kernel_args": "ro console=ttyS0 noapic reboot=k panic=1 pci=off nomodules systemd.journald.forward_to_console systemd.unit=firecracker.target init=/sbin/overlay-init",
  "root_drive": "/var/lib/firecracker-containerd/runtime/default-rootfs.img",
  "cpu_count": 1,
  "cpu_template": "T2",
  "log_levels": ["debug"],
  "jailer": {
    "runc_binary_path": "/usr/local/bin/runc"
  }
}
```

| log levels                     | description                                                                     |
| :---------------------------:  | :-----------------------------------------------------------------------------: |
| error                          | This will set all log levels to error                                           |
| warning                        | This will set all log levels to warning                                         |
| info                           | This will set all log levels to info                                            |
| debug                          | This will set all log levels to debug                                           |
| firecracker:error              | Logs any error information in Firecracker's lifecycle                           |
| firecracker:warning            | Logs any error and warning information in Firecracker's lifecycle               |
| firecracker:info               | Logs any error, warning, info information in Firecracker's lifecycle            |
| firecracker:debug              | Most verbose log level for firecracker                                          |
| firecracker:output             | Logs Firecracker's stdout and stderr. Can be used with other firecracker levels |
| firecracker-go-sdk:error       | Logs any errors information in firecracker-go-sdk                               |
| firecracker-go-sdk:warning     | Logs any errors or warnings in firracker-go-sdk                                 |
| firecracker-go-sdk:info        | Logs any errors, warnings, or infos in firecracker-go-sdk                       |
| firecracker-go-sdk:debug       | Most verbose logging for firecracker-go-sdk                                     |
| firecracker-containerd:error   | Logs any error information during the container/vm lifecycle                    |
| firecracker-containerd:warning | Logs any error level logs along with any warn level logs                        |
| firecracker-containerd:info    | Logs any error, warn, and info level logs                                       |
| firecracker-containerd:debug   | Most verbose logging for firecracker-containerd                                 |

The firecracker:XX are mutually exclusive with other firecracker-YY meaning only one of the log levels can be set at a time. 
However, firecracker:output may be set with other firecracker:YY settings.
The firecracker-containerd:XX are also mutually exclusive with other firecracker-containerd-YY levels
info, error, warning, and debug are mutually exclusive and only one can be set at a time.

```json
{
  "firecracker_binary_path": "/usr/local/bin/firecracker",
  "kernel_image_path": "/var/lib/firecracker-containerd/runtime/default-vmlinux.bin",
  "kernel_args": "ro console=ttyS0 noapic reboot=k panic=1 pci=off nomodules systemd.journald.forward_to_console systemd.unit=firecracker.target init=/sbin/overlay-init",
  "root_drive": "/var/lib/firecracker-containerd/runtime/default-rootfs.img",
  "cpu_count": 1,
  "cpu_template": "T2",
  "log:levels": ["info","firecracker:debug","firecracker-containerd:error"],
  "jailer": {
    "runc_binary_path": "/usr/local/bin/runc"
  }
}
```

The example above shows that setting the log levels to info, but specifies that
firecracker to be on a debug level and firecracker-containerd to be logging at
the error level
