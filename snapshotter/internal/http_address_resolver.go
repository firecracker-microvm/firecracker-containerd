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
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	proxyaddress "github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy/address"
)

var (
	port     int
	filepath string
	logger   *logrus.Logger
	store    map[string]networkAddress
)

type networkAddress struct {
	Network string `json:"network"`
	Address string `json:"address"`
}

func init() {
	flag.IntVar(&port, "port", 10001, "port to be used in the network address")
	flag.StringVar(&filepath, "map", "map.json", "filepath to map configuration")
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
//     "network": "tcp",
//     "address": "192.168.0.1:80"
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

	if err := loadStoreFromFile(filepath); err != nil {
		logger.WithError(err).Error()
		os.Exit(1)
	}

	http.HandleFunc("/address", queryAddress)
	httpServer := &http.Server{
		Addr: fmt.Sprintf(":%d", port),
	}

	logger.Info(fmt.Sprintf("http server serving at port %d", port))
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

func loadStoreFromFile(filepath string) error {
	store = make(map[string]networkAddress)
	contextLogger := logger.WithField("filepath", filepath)
	contextLogger.Info("opening file for read...")

	fileStream, err := ioutil.ReadFile(filepath)
	if err != nil {
		return err
	}

	contextLogger.Info("reading JSON from file...")
	return json.Unmarshal(fileStream, &store)
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
	networkAddress, ok := store[namespace]
	if !ok {
		http.Error(writ, "Socket path not found", http.StatusNotFound)
		return
	}

	writ.WriteHeader(http.StatusOK)

	response, err := json.Marshal(proxyaddress.Response{
		Network: networkAddress.Network,
		Address: networkAddress.Address,
	})
	if err != nil {
		http.Error(writ, "Internal server error", http.StatusInternalServerError)
		return
	}

	writ.Write(response)
}
