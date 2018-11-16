// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"log"
	"os"

	"github.com/containerd/containerd/runtime/v2/shim"
)

const SHIM_ID = "aws.firecracker.v1"

func main() {
	// TODO: use containerd logging
	f, err := os.OpenFile("./ctr-firecracker-shim.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	log.SetOutput(f)
	// TODO: what should the id here be?
	shim.Run(SHIM_ID, NewService)
	log.Println("Run exited with err: ", err)
}
