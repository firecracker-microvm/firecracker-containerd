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
	"net/url"

	"github.com/sirupsen/logrus"
)

// IsAgentOnlyIO checks whether Stdout target is agent only in order to allow
// log redirection entirely within the VM (currently cio.BinaryIO and cio.LogFile)
func IsAgentOnlyIO(stdout string, logger *logrus.Entry) bool {
	parsed, err := url.Parse(stdout)
	if err != nil {
		logger.WithError(err).Debugf("invalid URL %q", stdout)
		return false
	}

	switch parsed.Scheme {
	case "binary", "file":
		return true
	default:
		return false
	}
}

// nullIOProxy represents an empty IOProxy implementation for cases when there is no need to
// stream logs from the Agent through vsock.
type nullIOProxy struct{}

// NewNullIOProxy creates an empty IOProxy
func NewNullIOProxy() IOProxy {
	return &nullIOProxy{}
}

func (*nullIOProxy) start(proc *vmProc) (ioInitDone <-chan error, ioCopyDone <-chan error) {
	initCh := make(chan error)
	close(initCh)

	copyCh := make(chan error)
	close(copyCh)

	return initCh, copyCh
}
