### About
The `taskworkfow.go` file contains code to pull and unpack a container image
using the `naive` snaphotter, create a container and execute a task using the
`firecracker-containerd` runtime.

### Building
After checking out the `firecracker-containerd` repo, you can build this
example using the `make examples` command.

### Running
For a basic workflow, without any networking setup, just run the executable
built with the command in the previous section as super user:
```bash
$ sudo /path/to/firecracker-containerd/examples/taskworkflow
```

For a workflow with networking setup for the container, create a tap device
for the VM by following [these instructions](https://github.com/firecracker-microvm/firecracker/blob/master/docs/network-setup.md).

This will create a tap device named `tap0`. Now, edit the kernel args in the
firecracker-containerd config to set a static ip address from the
`172.16.0.1/24` subnet. The following example sets it to `172.16.0.2`.
** NOTE: This example will not work if you're running more than 1 container
on a host at the same time **
```bash
$ cat /etc/containerd/firecracker-runtime.json
{
    "firecracker_binary_path": "/usr/local/bin/firecracker",
    "socket_path": "./firecracker.sock",
    "kernel_image_path": "/var/lib/firecracker-containerd/runtime/vmlinux-2018-07-03",
    "kernel_args": "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw ip=172.16.0.2::172.16.0.1:255.255.255.0:::off::::",
    "root_drive": "/var/lib/firecracker-containerd/runtime/amzn2-firecracker-fc-ctrd-2.0.20190130.runc-x86_64.ext4",
    "cpu_count": 1,
    "cpu_template": "T2",
    "console": "stdio",
    "log_fifo": "/tmp/fc-logs.fifo",
    "log_level": "Debug",
    "metrics_fifo": "/tmp/fc-metrics.fifo"
}
```

Now, run the example by passing the `-ip` argument:
```bash
$ sudo /path/to/firecracker-containerd/examples/taskworkflow -ip 172.16.0.2
```

You should see output similar to this:
```
2019/02/11 21:02:48.813898 Creating contained client
2019/02/11 21:02:48.814324 Created contained client
2019/02/11 21:02:48.981848 Successfully pulled docker.io/library/nginx:latest image
2019/02/11 21:02:51.202941 Successfully created task: demo for the container
2019/02/11 21:02:51.202962 Completed waiting for the container task
2019/02/11 21:02:51.231901 Successfully started the container task
2019/02/11 21:02:54.232051 Executing http GET on 172.16.0.2
2019/02/11 21:02:54.233193 Response from [172.16.0.2]:
[<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
<style>
    body {
        width: 35em;
        margin: 0 auto;
        font-family: Tahoma, Verdana, Arial, sans-serif;
    }
</style>
</head>
<body>
<h1>Welcome to nginx!</h1>
<p>If you see this page, the nginx web server is successfully installed and
working. Further configuration is required.</p>

<p>For online documentation and support please refer to
<a href="http://nginx.org/">nginx.org</a>.<br/>
Commercial support is available at
<a href="http://nginx.com/">nginx.com</a>.</p>

<p><em>Thank you for using nginx.</em></p>
</body>
</html>
]
172.16.0.1 - - [11/Feb/2019:21:02:54 +0000] "GET / HTTP/1.1" 200 612 "-" "Go-http-client/1.1" "-"
```
