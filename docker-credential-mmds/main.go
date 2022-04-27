// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.
package main

import (
	"fmt"
	"os"

	"github.com/docker/docker-credential-helpers/credentials"
	"github.com/firecracker-microvm/firecracker-containerd/docker-credential-mmds/mmds"
)

func main() {
	helper, err := mmds.NewHelper()
	if err != nil {
		fmt.Fprintf(os.Stderr, "docker-credential-mmds: %s\n", err)
		os.Exit(1)
	}
	credentials.Serve(helper)
}
