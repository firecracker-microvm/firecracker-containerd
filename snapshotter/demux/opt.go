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

package demux

import (
	"encoding/json"
	"strconv"

	"github.com/containerd/containerd/snapshots"
)

// WithVSockPath sets the Firecracker vsock path
func WithVSockPath(vsockPath string) snapshots.Opt {
	return snapshots.WithLabels(map[string]string{VSockPathLabel: vsockPath})
}

// WithRemoteSnapshotterPort sets the vsock port that the remote-snapshotter
// is listening on for its snapshot server
func WithRemoteSnapshotterPort(port uint32) snapshots.Opt {
	return snapshots.WithLabels(map[string]string{RemoteSnapshotterPortLabel: strconv.FormatUint(uint64(port), 10)})
}

// WithProxyMetricsPort sets the vsock port that the remote-snapshotter
// is listening on for its metrics server
func WithProxyMetricsPort(port uint32) snapshots.Opt {
	return snapshots.WithLabels(map[string]string{MetricsPortLabel: strconv.FormatUint(uint64(port), 10)})
}

// WithProxyMetricLabels sets labels that will be attached to proxy metrics
func WithProxyMetricLabels(labels map[string]string) (snapshots.Opt, error) {
	serialized, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	return snapshots.WithLabels(map[string]string{
		MetricsLabelsLabel: string(serialized),
	}), nil
}
