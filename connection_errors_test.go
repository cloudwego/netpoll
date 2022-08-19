// Copyright 2021 CloudWeGo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package netpoll

import (
	"errors"
	"syscall"
	"testing"
)

func TestErrno(t *testing.T) {
	var err1 error = Exception(ErrConnClosed, "when next")
	MustTrue(t, errors.Is(err1, ErrConnClosed))
	Equal(t, err1.Error(), "connection has been closed when next")
	t.Logf("error1=%s", err1)

	var err2 error = Exception(syscall.EPIPE, "when flush")
	MustTrue(t, errors.Is(err2, syscall.EPIPE))
	Equal(t, err2.Error(), "broken pipe when flush")
	t.Logf("error2=%s", err2)
}
