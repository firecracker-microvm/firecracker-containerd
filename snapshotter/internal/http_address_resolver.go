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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/containerd/containerd/namespaces"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/firecracker-microvm/firecracker-containerd/firecracker-control/client"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	proxyaddress "github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy/address"
)

var (
	port               int
	remotePort         int
	containerdSockPath string
	logger             *logrus.Logger
)

func init() {
	flag.IntVar(&port, "port", 10001, "service port for address resolver")
	flag.StringVar(&containerdSockPath, "containerdSocket", "/run/firecracker-containerd/containerd.sock", "filepath to the containerd socket")
	flag.IntVar(&remotePort, "remotePort", 10000, "the remote port on which the remote snapshotter is listening")
	logger = logrus.New()
}

// Simple example of an HTTP service to resolve snapshotter namespace
// to a forwarding address for the demultiplexing snapshotter.
//
// Example:
// curl -X GET "http://localhost:10001/address?namespace=ns-1"
//
// Response:
// {
//     "network": "unix",
//     "address": "/var/lib/firecracker-containerd/shim-base/default#cbfad871-0862-4dd6-ae7a-52e9b1c16ede/firecracker.vsock"
// }
func main() {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		<-c
		cancel()
	}()

	if !flag.Parsed() {
		flag.Parse()
	}

	http.HandleFunc("/address", queryAddress)
	httpServer := &http.Server{
		Addr: fmt.Sprintf("127.0.0.1:%d", port),
	}

	logger.Info(fmt.Sprintf("http resolver serving at port %d", port))
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return httpServer.ListenAndServe()
	})
	g.Go(func() error {
		<-gCtx.Done()
		return httpServer.Shutdown(context.Background())
	})

	if err := g.Wait(); err != http.ErrServerClosed {
		logger.WithError(err).Error()
		os.Exit(1)
	}

	logger.Info("http: server closed")
}

func queryAddress(writ http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(writ, fmt.Sprintf("%s method not allowed", req.Method), http.StatusForbidden)
		return
	}

	writ.Header().Set("Content-Type", "application/json")

	keys, ok := req.URL.Query()["namespace"]

	if !ok {
		http.Error(writ, "Missing 'namespace' query", http.StatusBadRequest)
		return
	}

	namespace := keys[0]

	fcClient, err := client.New(containerdSockPath + ".ttrpc")
	if err != nil {
		logger.WithError(err).Error("could not create firecracker client")
		http.Error(writ, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer fcClient.Close()

	ctx := namespaces.WithNamespace(req.Context(), namespace)
	vmInfo, err := fcClient.GetVMInfo(ctx, &proto.GetVMInfoRequest{VMID: namespace})
	if err != nil {
		logger.WithField("VMID", namespace).WithError(err).Error("unable to retrieve VM Info")
		http.Error(writ, "Internal server error", http.StatusInternalServerError)
		return
	}

	writ.WriteHeader(http.StatusOK)

	response, err := json.Marshal(proxyaddress.Response{
		Network:         "unix",
		Address:         vmInfo.VSockPath,
		SnapshotterPort: strconv.Itoa(remotePort),
	})
	if err != nil {
		http.Error(writ, "Internal server error", http.StatusInternalServerError)
		return
	}

	writ.Write(response)
}
