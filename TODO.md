TODO
===

Snapshotter
---

The existing snapshotter is, by design, simple and broadly compatible
but not very efficient. We should implement an alternative snapshotter
following a model similar to Docker's device-mapper storage driver.

Networking
---

Integration of CNI into the appropriate components such that
MicroVM-enclosed containers have network access.

Because Firecracker requires the use of Linux tap devices, many
existing CNI plugins will not work. Design one or more plugins to
facilitate various network configurations.

Testing
---

Most components do not currently have any automated tests.
