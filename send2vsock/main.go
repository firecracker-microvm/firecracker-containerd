// Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"io"
	"os"
	"strconv"

	"github.com/mdlayher/vsock"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: send2vsock CID PORT\n")
		os.Exit(1)
	}

	cid, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid CID: %+v\n", err)
		os.Exit(1)
	}

	port, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid port: %+v\n", err)
		os.Exit(1)
	}

	conn, err := vsock.Dial(uint32(cid), uint32(port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to the vsock: %+v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	io.Copy(conn, os.Stdin)
}
