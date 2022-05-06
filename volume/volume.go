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

// Package volume provides volumes like Docker and Amazon ECS.
// Volumes are specicial directories that could be shared by multiple containers.
//
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_definition_parameters.html#volumes
package volume

// Volume is a special directory that could be shared between containers.
type Volume struct {
	name     string
	hostPath string
}

// New returns an empty volume.
func New(name string) *Volume {
	return &Volume{name: name}
}

// FromHost returns a volume which has the files from the given path.
func FromHost(name, path string) *Volume {
	return &Volume{name: name, hostPath: path}
}
