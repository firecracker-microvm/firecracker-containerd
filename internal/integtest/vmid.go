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

package integtest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// VMIDGen generates namespaced VMIDs that are unique across different VMIDGens.
// We have many tests which use a numeric ID as the VMID which could conflict with
// concurrently running test. VMIDGen guarantees uniqueness across different tests.
type VMIDGen struct {
	prefix string
}

// NewVMIDGen creates a new VMIDGen
func NewVMIDGen() (*VMIDGen, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return &VMIDGen{hex.EncodeToString(b)}, nil
}

// VMID creates a namespaced VMID given an integer seed.
// A VMIDGen will generate the same VMID multiple times given the same seed,
// but two different VMIDGens will generate different ids given the same seed.
func (gen *VMIDGen) VMID(seed int) string {
	return fmt.Sprintf("%s-%d", gen.prefix, seed)
}
