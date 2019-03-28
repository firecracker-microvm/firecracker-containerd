
# Building protobuf definitions

In order to use `sandbox` service (a `containerd` GRPC plugin) you need to use same protobuf generator as in `containerd`.
Do the following steps in order to prepare environment:

- Install `protoc`. Use [this script](https://github.com/containerd/containerd/blob/master/script/setup/install-protobuf) for convenience.
- Install `gogoprotobuf`:
  ```
  go get -u github.com/gogo/protobuf
  go get -u github.com/gogo/protobuf/protoc-gen-gofast
  ```
- Run make:
  ```
  make proto
  ```
