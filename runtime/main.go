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
