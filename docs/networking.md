# Firecracker-containerd VM Networking

# Background

## Problem

By default, a container started today via Firecracker-containerd will not have any access to networks outside the VM, whether to a local network on the host or the internet. Enabling up network connectivity between a Firecracker-containerd container and a network outside the VM is currently a manual process that requires creation of network devices, specification of IPs/routes/gateways on the host, specification of IPs/routes/gateways inside the VM and DNS configuration in the VM. The manual nature of this setup leaves significant complexity to be fully owned and managed by Firecracker-containerd users.

Additionally, while today our setup guide contains instructions on setting up one possible configuration for enabling networking in the VM, it does not provide a story on how to integrate with the many possible networking configurations a user might require, including those setup with CNI plugins. Users are currently left to always figure out these details on their own.

There is significant room for improvement here. The root of the issue is that the only interface Firecracker-containerd currently provides for configuring VM networking is asking the user for a name of an existing tap device to plug into the VM. Instead, we can present a higher-level interface that takes care of both creating VM tap devices and connecting the tap devices to networks outside the VM, including those created via CNI.

## Goals+Requirements

### Goals

1. An interface for configuring the network Firecracker-containerd containers will be presented inside VMs as their “host network namespace”. The interface should take care of creating the tap device for the VM and connecting the tap device to other networks on the host.
2. A clear story on how users can integrate the above interface with CNI plugins.

### Targeted Users

The new interface proposed here is targeted for:

1. Users that want networking to “just work” out of the box when using Firecracker-containerd (especially those trying it out the first time)
2. Users that are familiar with CNI and would benefit from having Firecracker-containerd manage CNI-based network setup for each of their container VMs

The new interface is specifically **not** targeting:

1. “Power users” that want very fine grained control over the network setup for each of their VMs.
    * These users can continue to use the existing interface of just specifying a pre-created tap device for their VMs

### Requirements

1. The existing user interface of just specifying a tap device to use should still be accessible to users that don’t want/need a higher-level interface
2. Avoid disrupting the customer’s existing network configuration on their host. Leave as little trace as possible unless the user explicitly configures otherwise.
3. Path to IPv6 support (though does not need to be implemented immediately)
4. Path to support for syncing of IP and DNS updates made after the VM has been created (though does not need to be implemented immediately)
5. Must integrate with Jailer

### Nice-To-Have

1. Clean integration with CRI support
    1. We don’t support CRI today and there are still open questions on what it might look like, so this is just something to consider in the abstract right now

## Subproblems

The overall goal+requirements can be broken down into a few subproblems that will be addressed one-by-one:

1. **Networking Model** - How is the tap device used by the Firecracker VM “hooked-up” to a network on the host?
2. **User Interface** - What do we ask from the user (instead of just a tap device name)?
3. **Internal VM Configuration** - How do we configure the VM guest kernel based on the networking configuration setup outside on the host? Namely:
    1. IP address, subnet and gateway
    2. DNS resolution (i.e. resolv.conf)

## Assumptions

1. Firecracker VMs only support tap devices as their virtual NIC on the host
2. If using a CNI plugin, multiple containers in a single ECS Task, K8s pod, etc. will all use the same network namespace configured with the given plugin.
    1. i.e. When using a CNI plugin, there is never a Task/Pod with containers that have different network namespaces
    2. This means that we’d never need to support a VM with containers that are each configured with different CNI plugins

# Options for Networking Model

This section is just about how a tap device given to the VM can be connected to a network on the host.

## A) (Preferred) Traffic Control

The Linux Kernel’s [Traffic Control (TC)](http://tldp.org/HOWTO/Traffic-Control-HOWTO/intro.html) system provides a very low-level but highly flexible interface for manipulating network packets and traffic flow across network devices on a single host.

Most relevant to our interests, the [U32 filter](http://man7.org/linux/man-pages/man8/tc-u32.8.html) provided as part of TC allows you to create a rule that essentially says “take all the packets entering the ingress queue of this device and move them to the egress queue of this other device”. For example, if you have DeviceA and DeviceB you can setup that rule on each of them such that the end effect is every packet sent into DeviceA goes out of DeviceB and every packet sent to DeviceB goes out of DeviceA. The host kernel just moves the ethernet packets from one device’s queue to the other’s, so the redirection is entirely transparent to any userspace application or VM guest kernel all the way down to and including the link layer.

* We first learned about this approach from [Kata Containers](https://github.com/kata-containers/runtime), who are using it for similar purposes in their framework. They have [some more background information documented here](https://gist.github.com/mcastelino/7d85f4164ffdaf48242f9281bb1d0f9b).
* Another use of TC redirect filters in the context of CNI plugins can be found in the [bandwidth CNI plugin](https://github.com/containernetworking/plugins/tree/master/plugins/meta/bandwidth).

This technique can be used to redirect between a Firecracker VM’s tap device and another device in the network namespace Firecracker is running in. If, for example, the VM tap is redirecting with a veth device in a network namespace, the VM guest internally gets a network device with the same mac address as the veth and needs to assign to it the same IP and routes the veth uses. After that, the VM guest essentially operates as though its nic is the same as the veth device outside on the host.

**Pros**

* Imposes the least requirements on the network Firecracker is running in relative to other options. The tap can be redirected with any device type and just reuses the same identity from the link layer up (same mac address, same IP, same routes, etc.).
    * No requirement for creating more devices in the network namespace other than the tap (which has to made no matter what)
    * No requirement for the VM to get its own IP on the network separate from what’s already configured in the network namespace
      * This makes CNI plugin chaining much easier (discussed in the Summary of Proposed Solution section)
* IPv6 support has been verified in prototypes

**Cons**

* TC is not very well documented (and at times entirely undocumented), which raises the overhead of maintaining and documenting our own code that uses it
* By default, there is no network connectivity available outside the VM but inside the network namespace after the redirect filter is setup (it only works inside the VM). This is because outside the VM, the IP is assigned to the device that is redirecting to the tap, so when you create a socket, it’s bound to the device whose ingress traffic is being sent into the VM (not to the actual network).
    * In many (likely vast majority of) use cases this should not be an issue as all containers run inside the VM anyways. It could only potentially affect a use case where someone wants to also run another process in the network namespace of the Firecracker VM. 
* If the user has custom qdiscs and tc filters attached to the egress of the device the tap is going to redirect with, they will be ignored after the tc filter is setup.
    * This is likely a fairly obscure case but may be addressable by having our code migrate qdiscs+filters to the tap device egress. More investigation would be required.

## B) Network Bridge

A typical pattern for connecting a VM tap device to an outside network is to attach the tap to a virtual network bridge, which can then serve as a switch between the tap device and other devices attached to the bridge.

The tap device and VM have their own MAC address and are thus treated as their own separate entities on the network they are bridged with, including being assigned their own IP. For some use cases, this is what’s desired anyways and is convenient. However, for other use cases this introduces a fair bit of complication and may not be universally compatible with all network setups.

**Pros**

* Better documentation than TC

**Cons**

* VM always requires getting its own IP on the user’s network, which can introduce extra complication depending on the network.

## Justification for Preferred Solution

TC Filtering is the preferred option as it supports the most use cases out of the box and imposes the fewest requirements on the network the Firecracker VM is joining. It also does not rule out adding support for network bridged tap devices in the future if there is ever a use case for it.

# Options for User Configuration

This section is about the actual interface users will see for setting up the network their Firecracker
VMs will execute in.

## A) (Preferred) Runtime Invokes CNI Plugins

In this option, Firecracker-containerd just asks for CNI configuration during a CreateVM call, which it will use to configure a network namespace for the Firecracker VM to execute in. The API updates may look something like:

```
message FirecrackerNetworkInterface {
    // <existing fields...>
    
    // CNI Configuration that will be used to configure the network interface
    CNIConfiguration CNIConfig;

    // Static configuration that will be used to configure the network interface
    StaticNetworkConfiguration StaticConfig;
}

message FirecrackerCNIConfiguration {
    // Name of the CNI network that will be used to configure the VM
    string NetworkName;
    
    // IF_NAME CNI parameter provided to plugins for the name of devices to create
    string InterfaceName;
    
    // Paths to CNI bin directories, CNI conf directory and CNI cache directory, 
    // respectively, that will be used to configure the VM.
    repeated string BinPath;
    string ConfDirectory;
    string CacheDirectory;
    
    // CNI Args passed to plugins
    repeated CNIArg Args;
}

message StaticNetworkConfiguration {
    string MacAddress;
    string HostDevName;
    IPConfiguration IPConfig;
}

message IPConfiguration {
    // Network configuration that will be applied to a network interface in a
    // Guest VM on boot.
    string PrimaryAddress;
    string GatewayAddress;
    repeated string Nameservers;
}
```

Additionally, a default CNI configuration can be specified in the Firecracker-containerd runtime config file, which will be used if none is specified in the CreateVM call. If no CNI configuration is specified in either the CreateVM call or the runtime configuration file, the behavior falls back to being the same as it is today.

Much of the implementation updates can be put in the Firecracker-Go-SDK and then just utilized by Firecracker-containerd, which has the nice side benefit of providing the features to all Go-SDK users.

It will be an error to specify both CNI configuration and a list of FirecrackerNetworkInterfaces; users must specify just one or neither.

In the implementation details, if a CNI Configuration is applied, the Runtime will (via the Go-SDK) execute CNI and use the output to generate values for FirecrackerNetworkInterfaces. The additional static IP configuration that can be provided as part of FirecrackerNetworkInterface is needed so CNI configuration can be applied, but it is also available to users who don’t want to use CNI but would get some benefit from being able to apply a static networking configuration to their VM anyways.

In our first implementation, if a CreateVM call specifies multiple FirecrackerNetworkInterfaces, it will be an error to specify StaticIPConfiguration for more than one of them (due to limitation of using `ip=...`). In the longer-run, we can consider updating the implementation to support configuring multiple network interfaces (such as by starting our own DHCP server that configures each VM interface based on mac address, similar to what [Ignite](https://github.com/weaveworks/ignite) does.)

**Pros:**

* Allows specification of a default CNI configuration to use, which opens several doors
    * Support for networking in single-container “default-path” VMs where the user never has to explicitly make a CreateVM call (i.e. `ctr run —runtime firecracker ...` or similar)
    * Potential support for setting up VM networking when using wrappers that don’t know about CreateVM (such as CRI)
* Simple integration with Jailer (if network namespace is specified via Jailer, it can just be additionally processed through the CNI config too)

**Cons:**

* Tied to CNI specifically
    * This option doesn’t rule out adding support for different ways of configuring the VM’s netns in the future however if the use case arises.

## B) User Creates NetNS

In this option, users can optionally provide a path to a network namespace file during CreateVM; the Firecracker-containerd runtime shim will create the Firecracker VM child process in the provided network namespace. If no path is provided, the network namespace is left unchanged from the shim’s.

Because the Firecracker-containerd runtime is agnostic to CNI, this option also requires the user specify DNS settings (such as nameserver) when creating their VM.

A sketch of the Firecracker-containerd API updates:

```
message FirecrackerDNSConfiguration {
    // DNS nameserver related configuration
}

message FirecrackerIPConfiguration {
    // IP+Route related configuration
}

message FirecrackerNetworkConfiguration {
    // (optional) path to a bind-mounted network namespace that the VM will be spawned in.
    // If unset, defaults to the network namespace containerd is running in.
    string NetworkNSPath;

    // (optional) configuration that will be written to the VM's /etc/resolv.conf
    // If unset, defaults to not overwriting whatever /etc/resolv.conf is included
    // in the VM image (if any).
    FirecrackerDNSConfiguration DNSConfiguration;

    // (optional) IP configuration that will be set on the nic seen by the VM guest.
    // If unset, defaults to not performing any configuration inside the VM
    FirecrackerIPConfiguration IPConfiguration;
 
    // The existing FirecrackerNetworkInterface configuration existing today
    // which specifies the name of the tap device on the host and rate limiters
    repeated FirecrackerNetworkInterface NetworkInterfaces;
}

message CreateVMRequest {
    // <same existing fields except FirecrackerNetworkInterface which is replaced with the following...>
    FirecrackerNetworkConfiguration NetworkConfiguration;
}
```

**Pros:**

* Keeps the runtime agnostic to framework used to create netns (i.e. CNI, CNM, manually, etc.). Users have the flexibility to use whatever framework they want in order to define the netns or do it their own custom way.

**Cons:**

* No clean way for users to specify a default way of generating a netns for a Firecracker VM. If we supported a default value for the path to the network namespace, every Firecracker VM would be spun up in there, which in turn requires it be pre-configured with 2 devices for every VM that the user would want to spin up (as TC mirroring only works in a one-to-one mapping between devices).
    * Cannot support the “default-path” of single-container VMs where the user doesn’t explicitly call CreateVM. Users must always use CreateVM in order to get network connectivity.
    * More difficult to integrate with containerd “wrappers” like CRI which are not aware of CreateVM and would thus never be able to configure a default way of generating a network namespace for a given container’s VM.

## Justification for Preferred Solution

Option A is preferred because it seems to result in the best user experience. It crucially supports defining a default way of generating a new network namespace for each VM, which helps users that are not specifically calling CreateVM.

While CNI is not the only framework for configuring network namespaces (Docker has its specific CNM spec too for example), it is the current “defacto” standard and is flexible enough to work with numerous different networking use cases. Integrating with it should satisfy the most users right away but also doesn’t inherently rule out supporting additional network namespace configuration frameworks in the future.

# Options for Internal VM Configuration

In order for networking to work as expected inside the VM, it needs to have IP and DNS settings that are compatible with the network outside the VM. Namely:

* IP subnet and default gateway assignments of the network device much match those of the outside device
* The DNS settings provided in /etc/resolv.conf should match what the outside CNI plugin specifies or, if the plugin does not specify any DNS settings, what the host has set in its /etc/resolv.conf.

## A) (Preferred) Kernel Boot Parameters

[The Linux kernel accepts boot parameters](https://www.kernel.org/doc/Documentation/filesystems/nfs/nfsroot.txt) for setting a static IP configuration and DNS configuration (the documentation makes it seem the support is specific to NFS, but in practice it can be used without NFS for configuring any kernel on boot).

The IP configuration is just pre-configured in the kernel when the system starts (the same end effect of having run the corresponding netlink commands to configure IP and routes). The DNS configuration is applied by writing the nameserver and search domain configuration to /proc/net/pnp in a format that is compatible with /etc/resolv.conf. The typical approach is to then have /etc/resolv.conf be a symlink to /proc/net/pnp.

Users of Firecracker-containerd are also free to provide their own kernel boot options, which could include their own static IP/DNS configuration. In those cases, if they have enabled CNI configuration, Firecracker-containerd will return an error.

**Pros**

* Simplest option available, just requires passing boot options to the Firecracker kernel which we can already do today.

**Cons**

* Imposes that the rootfs of the Firecracker VM (not the container rootfs) set /etc/resolv.conf to be a symlink to /proc/net/pnp
* If we want to support live updates to DNS+IP configuration in the future, there’s no clear path using this solution as the configuration is entirely static.
* [Cannot support ipv6 on most distributions](https://serverfault.com/a/449523)
* Cannot support configuring more than one interface (the VM Guest’s primary interface)

## B) MMDS

In this option, IP and DNS configuration can be set in MMDS, which the VM agent can read and use to assign IPs and write /etc/resolv.conf inside the VM.

The IP configuration can be provided in the form of some known JSON structure, which Agent parses and makes the appropriate netlink calls to apply inside the VM.

The DNS configuration can be provided by the runtime to the Agent as the full contents of /etc/resolv.conf, which Agent then just copies to /etc/resolv.conf inside the VM.

In the case of an error applying the configuration, Agent will not be able to directly tell the Runtime outside the VM something went wrong (as MMDS is not an RPC). It can write a log line and shut the VM down, which would then become apparent to callers when either their CreateVM call times out or they get a failure trying to create a container in the VM.

**Pros**

* If we want to support live updates to DNS+IP configuration in the future, there’s a pretty clear path to just update Agent to poll MMDS continuously and apply any updates it sees.
* Middle of road complexity (more complicated than Option A but less complicated than Option C+D)

**Cons**

* MMDS isn’t an RPC, which makes it difficult to communicate failures applying the configuration inside the VM. Users may have to inspect logs from inside the VM to see any failure messages.
* Requires careful namespacing of Firecracker-containerd’s internal use of MMDS in order to not conflict with any user’s data also being provided through MMDS

## C) Internal TTRPC API over VSock

In this option, Agent adds a new method to its TTRPC server, `SetNetworkConfiguration`, which provides IP and DNS configuration that Agent should apply to the VM (via netlink and overwriting /etc/resolv.conf).

This API would be internal, only used by our Runtime outside the VM to communicate with our Agent inside the VM. Users would not call this directly.

The Runtime outside the VM will call this API (if needed) during its CreateVM implementation. In the case of an error applying the network configuration, Agent can directly return the error details via the RPC result, which Runtime can then relay back to the user as an error message for a CreateVM failure.

**Pros**

* RPC Model results in errors being returned directly to clients, making it clear what caused their VM creation to fail if something goes wrong
* If we want to support live updates to DNS+IP configuration in the future, we can just have Runtime call this API again at any time an update needs to occur. 

**Cons**

* Agent’s API increases in size and complexity

## D) DHCP

In this option, the Guest VM configures its network interfaces via DHCP. The runtime can then start its own small DHCP server to provide configuration to the Guest VM. This approach is taken by the [Ignite](https://github.com/weaveworks/ignite) team.

**Pros**

* Supports configuring multiple interfaces (besides just the primary interface)
* Supports dynamic updates of IP+DNS configuration
* Does not rely on an Agent running in the VM (so the functionality could be put in the Go-SDK and then just re-used by the runtime)

**Cons**

* Fairly complex for the runtime to have to start its own DHCP server
* Might increase in boot times

## Justification for Preferred Solution

Option A is by far the simplest to implement in the short-term, making it a good starting point. While it doesn’t support use cases like dynamically updating the IP and DNS configuration, we also don’t have a need to support those uses cases right away.

The best path seems to be to go with Option A for now and consider Option B, C or D if we need to support dynamic updates and ipv6 in the future, at which time we can better assess the requirements for those features. Given the low overhead of implementing Option A, the throw-away effort is minimal.

The biggest immediate downside of Option A is the requirement that /etc/resolv.conf be a symlink to /proc/net/pnp. However, this is only a requirement for the VM rootfs, not container rootfs. We can easily implement it in our recommended image builder and document that users with different rootfs images just be aware this symlink needs to be created if they want DNS settings to be propagated from CNI configuration outside the VM.

# Summary of Proposed Solution

Firecracker-containerd will build the current binaries it does today plus a new CNI-plugin compatible binary, `tc-redirect-tap`, that takes an existing network namespace and creates within it a tap device that is redirected via a TC filter to an already networked device in the netns. This CNI plugin is only useful when chained with other CNI-plugins (which will setup the device that the tap will redirect with).

When setting up Firecracker-containerd, users can optionally include a set of default network interfaces to provide to a VM if none are specified by the user. This allows users to optionally set their VMs to use CNI-configured network interfaces by default. The user is free to provide an explicit NetworkInterfaces list during the CreateVM call (including an empty list), in which case that will be used instead of any defaults present in the runtime config file.

The Firecracker Go SDK will take care of checking whether any Jailer config specifies a pre-existing network namespace to use and, if not, creating a new network namespace for the VM on behalf of the user. The Go SDK will also take care of invoking CNI on that network namespace, starting the VMM inside of it, and handling CNI network deletion after the VM stops.

If CreateVM succeeds, any containers running inside the VM with a “host” network namespace will have access to the network configured via CNI outside the VM.

The CNI configuration Firecracker-containerd requires from users are a CNI network name and an IfName parameter to provide to CNI plugins. Other values such as the a CNI bin directories and CNI configuration directories can be provided but will have sensible defaults if not provided.

A hypothetical example CNI configuration file that uses the standard [ptp CNI plugin](https://github.com/containernetworking/plugins/tree/master/plugins/main/ptp) to create a veth device whose traffic is redirected with a tap device:

```
{
  "cniVersion": "0.3.1",
  "name": "fcnet",
  "plugins": [
    {
      "type": "ptp", // This sets up a veth pair with one end on the host and one end in the netns
      "ipMasq": true,
      "ipam": {
        "type": "host-local",
        "subnet": "192.168.1.0/24",
        "resolvConf": "/etc/resolv.conf"
      }
    },
    {
      "type": "tc-redirect-tap" // creates a tap device redirected with the veth pair created in the previous step
    }
  ]
}
```

Given the above configuration, the containers inside the VM will have access to the 192.168.1.0/24 network. Thanks to setting `ipMasq: true`, the containers should also have internet access (assuming the host itself has internet access).

Firecracker-containerd will also provide an example CNI configuration that, if used, will result in Firecracker VMs being spun up with the same access to the network the host has on its default interface (something comparable to Docker’s default networking configuration). This can be setup via a Makefile target (i.e. `demo-network`), which allows users trying out Firecracker-containerd to get networking, including outbound internet access, working in their Firecracker VMs by default if they so choose.

## Hypothetical CRI interactions

Though Firecracker-containerd as a whole has not figured out the entire story of how to integrate with CRI, it’s worth considering what the interaction may look like in terms of CNI. This section is not intended to answer every question though; it just has some initial thoughts.

CRI works today by taking the CNI configuration (and all other pod configuration) and using it to create an initial “sandbox” container for the pod. When actual containers from the user are created in the pod, their network namespace is set to the same as this initial “sandbox” container. Thus, in the context of Firecracker-containerd, the CNI configuration would likely be happening *inside* the VM (as that’s where the sandbox container would exist), not outside of it like is proposed in this doc.

On a theoretical level, this doesn’t fundamentally conflict with the model proposed here (using CNI to configure the network the entire VM is operating in on the host). You can use CNI to hook up the Firecracker tap to a host network and additionally use separate CNI configuration inside the VM to configure another layer of networking. A hypothetical scenario where this actually makes sense is to use the Firecracker-containerd CNI configuration to simply give the VM network access and use the CRI CNI configuration to configure an overlay network on top of the VM’s network.

However, this nesting of CNI configuration certainly raises alarms in terms of complexity and confusion for users. When looking into CRI support, we should evaluate whether there are other options such as finding a way to have CRI configure its CNI configuration outside the VM, though it’s unclear how that could be accomplished today without significant changes to CRI itself.

# Appendix

## Performance Comparisons

Getting solid comparisons between a TC filter setup and a network bridge setup is still a work in progress; this section will be updated as those arrive.

***Preliminary*** results are showing that TC filter setups use 10-20% fewer CPU cycles (in terms of user, system and guest) for the same bandwidth going through a software bridge setup. This won't be considered conclusive until we have a production implementation of everything to get final numbers with.
