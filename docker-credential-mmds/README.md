# Docker Credential Helper MMDS

Docker Credential Helper MMDS is a [credential helper](https://github.com/docker/docker-credential-helpers) for exposing docker credentials inside a [Firecracker microVM](https://github.com/firecracker-microvm/firecracker) via [MMDS](https://github.com/firecracker-microvm/firecracker/blob/main/docs/mmds/mmds-user-guide.md).

# Building

`docker-credential-mmds` can built with
```
make
```

# Configuration

## Docker inside Firecracker
When building a firecracker rootfs, place `docker-credential-mmds` on the `PATH` then put the following configuration in `~/.docker/config`

```
{
	"credsStore": "mmds"
}
```

This configures the Docker daemon running inside the Firecracker microVM to read all credentials from MMDS.

## Credentials from Host

`docker-credential-mmds` reads credentials from MMDS inside the Firecracker microVM, but a cooperating process on the host needs to place credentials into MMDS. The credentials must be placed in MMDS under a key called `docker-credentials` which contains maps of host names to `username` and `password`. 

For example, the following configures credentials for the ECR public gallery and docker hub
```
{
	"docker-credentials": {
		"public.ecr.aws": {
			"username": "123456789012",
			"password": "access_key"
		},
		"docker.io": {
			"username": "user",
			"password": "pass"
		}
	}
}
```

### Placing credentials with the Firecracker HTTP API
One way to put credentials into MMDS is with firecracker's HTTP API.

```
curl --unix-socket /tmp/firecracker.socket -i \
    -X PUT "http://localhost/mmds"            \
    -H "Content-Type: application/json"       \
    -d '{
		"docker-credentials": {
			"public.ecr.aws": {
				"username": "123456789012",
				"password": "access_key"
			},
			"docker.io": {
				"username": "user",
				"password": "pass"
			}	
		}
	}'
```

### Placing credentials with the Firecracker-go-sdk
For larger systems, it may be useful to write a full program on the host to enable additional features such as credential refreshing. The [firecracker-go-sdk](https://github.com/firecracker-microvm/firecracker-go-sdk) wraps the firecracker HTTP APIs with go APIs for this purpose.

```
credentials := `{
	"docker-credentials": {
		"public.ecr.aws": {
			"username": "123456789012",
			"password": "access_key"
		},
		"docker.io": {
			"username": "user",
			"password": "pass
		}
	}
}`
fcClient, _ := client.New("/tmp/firecracker.socket")
fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
		VMID:     vmID,
		Metadata: credentials,
	})
```