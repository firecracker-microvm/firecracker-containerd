# Scaling the number of Firecracker microVMs per host

To scale the number of microVMs past one thousand, one needs to properly configure 
the system constraints and provision enough networking resources for the microVMs.

On Ubuntu 18.04 Linux, one needs to properly configure the number of processes, threads, memory, 
open files that may simultaneously exist in a system as well as create enough 
virtual bridges (one bridge can serve up to 1023 interfaces).

To configure the system, one needs to set up the following parameters. 
Note that the exact values depend on your setting.

In `/etc/security/limits.conf`, set `nofile`, `nproc` and `stack` to the 
appropriate values for both normal users and root:
```
* soft nofile 1000000
* hard nofile 1000000
root soft nofile 1000000
root hard nofile 1000000
* soft nproc 4000000
* hard nproc 4000000
root soft nproc 4000000
root hard nproc 4000000
* soft stack 65536
* hard stack 65536
root soft stack 65536
root hard stack 65536
```

Additionally, one needs to provision the ARP cache to avoid garbage collection.
```
sudo sysctl -w net.ipv4.neigh.default.gc_thresh1=1024
sudo sysctl -w net.ipv4.neigh.default.gc_thresh2=2048
sudo sysctl -w net.ipv4.neigh.default.gc_thresh3=4096
sudo sysctl -w net.ipv4.ip_local_port_range="32769 65535"
```

Also, configure the maximum number of processes and threads in the system.
```
sudo sysctl -w kernel.pid_max=4194303
sudo sysctl -w kernel.threads-max=999999999
```

Finally, configure the number of tasks. 
To configure system-wide, uncomment and set `DefaultTasksMax=infinity`in `/etc/systemd/system.conf`.
One also needs to set `UsersTasksMax=4000000000` in `/etc/systemd/logind.conf` 
(note that `infinity` is not a valid value here).

To configure the bridges for CNI, take a look at `demo-network` target in [Makefile](https://github.com/firecracker-microvm/firecracker-containerd/blob/master/Makefile)
and replicate the code to create enough bridges (1 bridge can have up to 1023 interfaces attached).
