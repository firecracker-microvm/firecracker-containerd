## How to run snapshotters benchmark

- `firecracker-containerd` project contains CloudFormation template to run an EC2 instance suitable for benchmarking.
It installs dependencies, prepares EBS volumes with same performance characteristics, and creates thin-pool device.
You can make stack with the following command (note: there is a charge for using AWS resources):

```bash
aws cloudformation create-stack \
    --stack-name benchmark-instance \
    --template-body file://benchmark_aws.yml \
    --parameters \
        ParameterKey=Key,ParameterValue=SSH_KEY \
        ParameterKey=SecurityGroups,ParameterValue=sg-XXXXXXXX \
        ParameterKey=VolumesSize,ParameterValue=20 \
        ParameterKey=VolumesIOPS,ParameterValue=1000
```

- You can find an IP address of newly created EC2 instance in AWS Console or via AWS CLI:

```bash
$ aws ec2 describe-instances \
    --instance-ids $(aws cloudformation describe-stack-resources --stack-name benchmark-instance --query 'StackResources[*].PhysicalResourceId' --output text) \
    --query 'Reservations[*].Instances[*].PublicIpAddress' \
    --output text
```

- SSH to an instance and prepare `firecracker-containerd` project:

```bash
ssh -i SSH_KEY ec2-user@IP
mkdir /mnt/disk1/data /mnt/disk2/data /mnt/disk3/data
cd
git clone https://github.com/firecracker-microvm/firecracker-containerd.git
cd firecracker-containerd
make
```

- Now you're ready to run the benchmark:

```bash
sudo su -
cd snapshotter/
go test -bench . \
    -dm.thinPoolDev=bench-docker--pool \
    -dm.rootPath=/mnt/disk1/data \
    -overlay.rootPath=/mnt/disk2/data \
    -naive.rootPath=/mnt/disk3/data
```

- The output will look like:

```bash
goos: linux
goarch: amd64
pkg: github.com/firecracker-microvm/firecracker-containerd/snapshotter

BenchmarkNaive/run-4               1     177960054662 ns/op	   0.94 MB/s
BenchmarkNaive/prepare             1      11284453889 ns/op
BenchmarkNaive/write               1     166035272412 ns/op
BenchmarkNaive/commit              1        640126721 ns/op

BenchmarkOverlay/run-4             1       1019730210 ns/op	 164.53 MB/s
BenchmarkOverlay/prepare           1         26799447 ns/op
BenchmarkOverlay/write             1        968200363 ns/op
BenchmarkOverlay/commit            1         24582560 ns/op

BenchmarkDeviceMapper/run-4        1       3139232730 ns/op	  53.44 MB/s
BenchmarkDeviceMapper/prepare	   1       1758640440 ns/op
BenchmarkDeviceMapper/write        1       1356705388 ns/op
BenchmarkDeviceMapper/commit       1         23720367 ns/op

PASS
ok  	github.com/firecracker-microvm/firecracker-containerd/snapshotter	185.204s
```

- Don't forget to tear down the stack so it does not continue to incur charges:

```bash
aws cloudformation delete-stack --stack-name benchmark-instance
```
