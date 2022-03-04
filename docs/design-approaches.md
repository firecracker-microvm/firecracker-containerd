# Design approaches in firecracker-containerd

This document describes possible design approaches for multi-container support with Firecracker and Containerd.


## Goals

Support running multiple containers inside a microVM in order to handle Orchestrator tasks with multiple container
definitions.


## Requirements

1. Orchestrator needs to launch multiple containers in the same VM. The container launch can be staggered. For example,
   requests could be something like: Launch container A in a VM. After a period of time (once the orchestrator verifies
   that container A is healthy), launch container B in the same VM.
2. Orchestrator needs the VM handle in some way. This is required for:
   - Invoking the firecracker MMDS API (for updating IAM Role credentials and metadata)
   - Draining VM log and metrics fifos
3. A VM should not be terminated as long as there are some containers running in it (unless it's an impairment event
   like reboot)


## Glossary

* Containerd - OCI compatible container runtime. Receives container lifecycle/management requests and performs them
  using configured plugins/options.
* Runtime - An implementation of the runtime v2 [spec](https://github.com/containerd/containerd/tree/main/runtime/v2)
  for containerd. Unlike the default implementation which runs runc directly, our runtime will be responsible for
  managing Firecracker instances as well as communicating container requests to the inside-the-vm-agent.
* Agent - A process inside of the running VM. Manages containers, handles communication with the Runtime and utilizes
  runc to launch containers inside the microVM.
* Orchestrator - An entity that manages tasks and submits requests to lower level subsystems. This is where all the
  decisions should be made as it is the piece with the largest amount of information about the container-group execution
  needs. It's where we can decide vm size, etc.
* VMID - abstract identifier not related to any specific implementation. Means a group of containers somehow related by
  same id (for example a group of containers from pod or task definition)


## High-level overview

![design-overview](img/design-overview.png)

1. Orchestrator accepts task definition request from control-plane and breaks it into sub-requests suitable for sending to
   containerd (using `CreateTaskRequest` [struct](https://github.com/containerd/containerd/blob/main/api/services/tasks/v1/tasks.pb.go#L79),
   additional parameters can be passed to runtime though `Options` [field](https://github.com/containerd/containerd/blob/main/api/services/tasks/v1/tasks.pb.go#L93))
2. containerd uses firecracker-containerd runtime and block device snapshotter. Requests are passed to runtime. Runtime
   is responsible for communicating with Agent, attaching/managing block devices and controlling container execution
   lifecycle.
3. Runtime controls the list of drives to use for passing container mounts (more on this in the following sections).
4. Runtime and/or FC-control starts the VM (unless it's already started), waits for the VM to boot up and Agent to
   start responding to requests via vsock channel. After that the Runtime submits requests for managing containers
   through Agent (interface is described below).
5. Agent accepts create container request parameters, mounts given block device (for example in
   `/containers/{id}/rootfs/`), saves bundle json to `/containers/{id}/`, and runs container using runc.
6. Orchestrator communicates with Firecracker microVM instance API and Agent using `vmHandle` to perform various tasks.
   FC control adds an abstraction layer for orchestrator and simplifies communication with FC instance and Agent by
   reusing Runtime/Containerd logic.


## Current Firecracker limitations

### Block devices

Attaching block device is painful in Firecracker and has lots of limitations. There is no hot-plug, so you have to
attach all block devices before running the microVM (and need to know the number of drives to be used in advance).
Though Firecracker allows to update drive properties (e.g. replace block device image) when the VM is already running,
so there are workarounds possible to run new containers inside the already running VM machine.

There is also no easy way to match drive id (which is unique drive identifier used in Firecracker API when attaching or
updating block device properties) with block device itself inside the VM (which has assigned `MAJOR:MINOR` numbers and
block device name). So when new container image is attached there is no straight way to tell the agent which block
device inside the VM to mount.

### Vsock

Vsock is experimental in Firecracker and final implementation may influence runtime → agent communication.
This doc relies on firecracker `0.12` vsock implementation. As Vsocks are very similar to regular sockets,
any communication protocol can be chosen (ttrpc, grpc, http, ...) for exchanging information.


# Design approaches

* Orchestrator
* Multiplexing
* Implement drive management for Firecracker to mount multiple containers, add drive matching functionality to Agent.
* Extend Agent service to work with multiple containers.

![design-sequence-diagram](img/design-sequence-diagram.png)


## Orchestrator

The orchestrator knows about task definitions (e.g. aware about high level picture), makes decisions based on given
information, and submits requests to containerd.

The orchestrator talks to all subsystems using containerd's [APIs](https://github.com/containerd/containerd/tree/main/api).
In order to manage containers lifecycle it can use [task](https://github.com/containerd/containerd/tree/main/services/tasks)
service, extra configuration can be specified thought option [fields](https://github.com/containerd/containerd/blob/main/api/services/tasks/v1/tasks.proto#L74).
Missing functionality (like firecracker control service) can be added as an extension to `containerd`.
Containerd supports GRPC plugins, so FC control can reuse existing GRPC [server](https://github.com/containerd/containerd/blob/a15b6e2097c48b632dbdc63254bad4c62b69e709/services/server/server.go#L78)
and live in same process as `containerd`. Orchestrator's client uses same GRPC connection (`containerd` has
`containerd.NewWithConn` to use existing connection [object](https://github.com/containerd/containerd/blob/09da2d867aff8e0bff40dbb94c539e44529aa192/client.go#L144))
for both `containerd` and FC control service. FC control and orchestrator share the same protobuf definition for
client/server communication.

A few details on Orchestrator / Runtime responsibilities:

* Runtime/FC control manages Firecracker microVMs lifecycle e.g. creates new VM instances for new create container
  requests, knows how to reuse existing VMs, and decides when to shutdown instances.
* Orchestrator uses a `VMID` for each container group and includes it in requests to Runtime to identify which microVM
  instance to use for serving a request. Runtime is responsible for matching given `VMID` with corresponding VM instance.
* Orchestrator is able to get running microVM instances and query specific VM through containerd's service layer (get
  metrics, set metadata, get logs, etc) by `VMID`.
* Runtime is responsible for tracking containers states and for VM termination if there are no running containers left.
* Orchestrator is responsible for pulling container images via `containerd` client.

## Multiplexing

There are lots of data need to be transferred from Agent to Runtime (agents logs, for each container: stream container
events/stdin/stdout/stderr). There are two major ways how this communication can be done:

### Separate connection for each stream of data

Agent creates several vsock channels and sends data separately (1 x service, ContainerCount x 3 stdin/out/err, 1 x logs).
Runtime connects to Agent, reads data from socket channels and forwards to containerd/logfiles/etc.

**Pros**:
* Simplicity, no multiplexing needed.

**Cons**:
* Need to manage low-level socket connection, potentially handle retires, timeouts, encryption(?), etc.
* Needs ports negotiation for each connection to let Runtime know which channel to listen to (and what kind of data to expect).
* Final vsock design in `Firecracker` not clear ([issue to track](https://github.com/firecracker-microvm/firecracker/issues/650)).
  It can be either one `AF_UNIX` socket per guest or one `AF_UNIX` socket per `AF_VSOCK` port. In case of one socket per
  microVM it's not possible to send data via separate channels.

### Use GRPC/TTRPC

Rely on GRPC streaming [capabilities](https://grpc.io/docs/guides/concepts.html#server-streaming-rpc).
In this case the only modification of proto interface is needed, GRPC generates everything else.
Runtime knows how to stream data from Agent via strongly typed interface (for example
`ReadStdout(containerID) returns (ReadStreamResponse)`).

**Pros**:
* GRPC/TTRPC can generate strongly typed interface for data exchange (see service example below)
* No need in port negotiation, Agent's well-known port always used.

**Cons**:
* TTRPC doesn't support streaming (need to implement and submit a PR)
* GRPC is heavier, potentially it consumes more resources ([need to clarify how much heavier it'll be for just one service](https://github.com/xibz/GRPCvsTTRPC/blob/master/README.md)).
* Multiplexing (in general) may consume more memory & CPU

### Multiplexing at the connection layer

It's possible to perform multiplexing at the connection layer instead of the application layer.
Firecracker team is exploring options around Firecracker handling some aspects of the communication
(i.e., exposing the correct vsock device inside the VM) and the outer application needing to handle other aspects
(i.e., (de)multiplexing vsock datagrams from an `AF_UNIX` socket on the host.

**Pros**:
* Simplicity, no multiplexing needed at the application level.

## Block devices

Because there is no way to attach block devices upon request, the suggested approach is to use fake block devices (`/dev/null` or
sparse files) to reserve drive ids before running Firecracker instance. When container needs to be run, fake device is
replaced (via [PatchGuestDriveByID](https://github.com/firecracker-microvm/firecracker-go-sdk/blob/main/firecracker.go#L238))
with real container image received as [mount](https://github.com/firecracker-microvm/firecracker-containerd/blob/0e39b738ab466358b1059bb283b19726476310a1/runtime/service.go#L651)
from snapshotter. Fake device can be represented by `/dev/null`, however Firecracker doesn't allow to attach same block
device more than once (even with different drive ids). So alias should be used. Another approach is to use sparse files
(Firecracker accepts regular sparse files as block devices unless file size <128).

Another challenge with block devices is that Agent needs to recognize block device inside the VM by drive ID, that comes
from runtime in create container request. There is no direct way of matching block device, so there are several
approaches might be taken:

1. Rely on order of attachment. When sequentially attaching drives `drive 1`, `drive 2`, `drive 3` in `Firecracker`,
   they appear inside the VM as:
   ```
   vda 254:0   12M disk / ← root device
   vdb 254:16 512B disk ← 1
   vdc 254:32   1K disk ← 2
   vdd 254:48   2K disk ← 3
   ```
   In current implementation `(0.12)` devices `vdb`, `vdc`, and `vdd` will match `drive 1`, `drive 2`, and `drive 3`
   respectively. So Agent, during initialization phase, can list available block devices (via `lsblk`), sort them by
   `MAJOR:MINOR`, and match with drive ids.
   **Pros**: very fast
   **Cons**: there is no order guarantee and this logic might be changed in future releases of Firecracker (risk).

2. Another approach is to use fake files instead of `/dev/null`. In this case the corresponding drive id can be written
   at the beginning of each file. During the initialization phase agent can list block devices, opens each block device and
   reads its drive id. Hence it can match drive id with the corresponding `MAJOR:MINOR` or device name inside the VM.
   This approach requires a bit more preparations, but guarantees the match.
   **Pros**: guarantees match, does not depend on implementation.
   **Cons**: requires preparation of fake devices, potentially increases initialization time.


## Communication between Runtime and Agent

In POC implementation runc's Task [service](https://github.com/containerd/containerd/blob/30b6f460b96137947b3de5ec92134d56cb763708/runtime/v2/task/shim.proto#L18)
was used for container orchestration. While it's fine to use it for managing just one container, it's quite limited when
there are multiple containers and/or there is a need to use additional features. The proposal is to refactor existing
interface for better handling of multiple containers, networking, stdio, etc. For example, agent interface may look like
this (taken from [Kata](https://github.com/kata-containers/agent/blob/master/protocols/grpc/agent.proto)):

```
service Agent {
    // execution
    rpc CreateContainer(CreateContainerRequest) returns (google.protobuf.Empty);
    rpc StartContainer(StartContainerRequest) returns (google.protobuf.Empty);

    rpc RemoveContainer(RemoveContainerRequest) returns (google.protobuf.Empty);
    rpc ExecProcess(ExecProcessRequest) returns (google.protobuf.Empty);
    rpc ListProcesses(ListProcessesRequest) returns (ListProcessesResponse);
    rpc UpdateContainer(UpdateContainerRequest) returns (google.protobuf.Empty);
    rpc StatsContainer(StatsContainerRequest) returns (StatsContainerResponse);
    rpc PauseContainer(PauseContainerRequest) returns (google.protobuf.Empty);
    rpc ResumeContainer(ResumeContainerRequest) returns (google.protobuf.Empty);

    // stdio
    rpc WriteStdin(WriteStreamRequest) returns (WriteStreamResponse);
    rpc ReadStdout(ReadStreamRequest) returns (ReadStreamResponse);
    rpc ReadStderr(ReadStreamRequest) returns (ReadStreamResponse);
    rpc CloseStdin(CloseStdinRequest) returns (google.protobuf.Empty);
    rpc TtyWinResize(TtyWinResizeRequest) returns (google.protobuf.Empty);

    // networking
    rpc UpdateInterface(UpdateInterfaceRequest) returns (types.Interface);
    rpc UpdateRoutes(UpdateRoutesRequest) returns (Routes);
    rpc ListInterfaces(ListInterfacesRequest) returns(Interfaces);
    rpc ListRoutes(ListRoutesRequest) returns (Routes);

    // misc
    rpc CopyFile(CopyFileRequest) returns (google.protobuf.Empty);
}

message CreateContainerRequest {
    string container_id = 1;
    string exec_id = 2;
    repeated Device devices = 4;
    Spec OCI = 6;
}

// Device represents only the devices that could have been defined through the
// Linux Device list of the OCI specification.
message Device {
    string id = 1;
    string type = 2;
    string vm_path = 3;
    string container_path = 4;
    repeated string options = 5;
}
```
