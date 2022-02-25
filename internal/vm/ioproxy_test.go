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

package vm

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fileConnector(path string, flag int) IOConnector {
	return func(procCtx context.Context, logger *logrus.Entry) <-chan IOConnectorResult {
		returnCh := make(chan IOConnectorResult, 1)
		defer close(returnCh)

		file, err := os.OpenFile(path, flag, 0600)
		returnCh <- IOConnectorResult{
			ReadWriteCloser: file,
			Err:             err,
		}

		return returnCh
	}
}

func TestProxy(t *testing.T) {
	dir := t.TempDir()

	ctx := context.Background()
	content := "hello world"

	err := ioutil.WriteFile(filepath.Join(dir, "input"), []byte(content), 0600)
	require.NoError(t, err)

	pair := &IOConnectorPair{
		ReadConnector:  fileConnector(filepath.Join(dir, "input"), os.O_RDONLY),
		WriteConnector: fileConnector(filepath.Join(dir, "output"), os.O_CREATE|os.O_WRONLY),
	}
	initCh, copyCh := pair.proxy(ctx, logrus.WithFields(logrus.Fields{}), 0)

	assert.Nil(t, <-initCh)
	assert.Nil(t, <-copyCh)

	bytes, err := ioutil.ReadFile(filepath.Join(dir, "output"))
	require.NoError(t, err)
	assert.Equal(t, content, string(bytes))
}
