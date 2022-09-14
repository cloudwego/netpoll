// Copyright 2022 CloudWeGo Authors
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

package uring

import (
	"errors"
	"io/ioutil"
	"math"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const openFile = "./../go.mod"

func MustNil(t *testing.T, val interface{}) {
	t.Helper()
	Assert(t, val == nil, val)
	if val != nil {
		t.Fatal("assertion nil failed, val=", val)
	}
}

func MustTrue(t *testing.T, cond bool) {
	t.Helper()
	if !cond {
		t.Fatal("assertion true failed.")
	}
}

func Equal(t *testing.T, got, expect interface{}) {
	t.Helper()
	if got != expect {
		t.Fatalf("assertion equal failed, got=[%v], expect=[%v]", got, expect)
	}
}

func Assert(t *testing.T, cond bool, val ...interface{}) {
	t.Helper()
	if !cond {
		if len(val) > 0 {
			val = append([]interface{}{"assertion failed:"}, val...)
			t.Fatal(val...)
		} else {
			t.Fatal("assertion failed")
		}
	}
}

func TestClose(t *testing.T) {
	u, err := IOURing(8)
	MustNil(t, err)
	Assert(t, u.Fd() != 0)
	defer u.Close()

	f, err := os.Open(openFile)
	MustNil(t, err)
	defer f.Close()

	err = u.Queue(Close(f.Fd()), 0, 0)
	MustNil(t, err)

	_, err = u.Submit()
	MustNil(t, err)

	cqe, err := u.WaitCQE()
	MustNil(t, err)
	MustNil(t, cqe.Error())

	_, err = unix.FcntlInt(f.Fd(), unix.F_GETFD, 0)
	Equal(t, err, unix.EBADF)
}

func TestReadV(t *testing.T) {
	u, err := IOURing(8)
	MustNil(t, err)
	defer u.Close()

	f, err := os.Open(openFile)
	MustNil(t, err)
	defer f.Close()

	v, err := makeV(f, 16)
	MustNil(t, err)

	err = u.Queue(ReadV(f.Fd(), v, 0), 0, 0)
	MustNil(t, err)

	_, err = u.Submit()
	MustNil(t, err)

	cqe, err := u.WaitCQE()
	MustNil(t, err)
	MustNil(t, cqe.Error())

	expected, err := ioutil.ReadFile(openFile)
	MustNil(t, err)
	Assert(t, vToString(v) == string(expected))
}

func TestReady(t *testing.T) {
	u, err := IOURing(8)
	MustNil(t, err)
	defer u.Close()

	Equal(t, u.cqRing.ready(), uint32(0))

	err = queueSQEs(u, 5, 0)
	Equal(t, u.cqRing.ready(), uint32(5))

	u.CQESeen()
	Equal(t, u.cqRing.ready(), uint32(4))

	u.Advance(4)
	Equal(t, u.cqRing.ready(), uint32(0))
}

func TestTimeoutWait(t *testing.T) {
	u, err := IOURing(8)
	MustNil(t, err)
	defer u.Close()

	err = u.Queue(Nop(), 0, 1)
	MustNil(t, err)

	if u.Params.features&IORING_FEAT_EXT_ARG != 0 {
		n, err := u.Submit()
		MustNil(t, err)
		Equal(t, n, uint(1))
	}

	n := 0
	for {
		cqe, err := u.WaitCQETimeout(time.Second)
		if errors.Is(err, syscall.ETIME) {
			break
		}
		if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
			runtime.Gosched()
			continue
		}

		MustNil(t, err)
		u.CQESeen()

		MustNil(t, cqe.Error())
		n++
	}
	Equal(t, n, 1)
}

func TestPeekCQE(t *testing.T) {
	u, err := IOURing(8)
	MustNil(t, err)
	defer u.Close()

	cqeBuff := make([]*URingCQE, 128)

	n := u.PeekBatchCQE(cqeBuff)
	Equal(t, n, 0)

	err = queueSQEs(u, 4, 0)
	MustNil(t, err)

	n = u.PeekBatchCQE(cqeBuff)
	Equal(t, n, 4)

	for i := 0; i < 4; i++ {
		Equal(t, cqeBuff[i].UserData, uint64(i))
	}

	err = queueSQEs(u, 4, 4)
	MustNil(t, err)

	u.Advance(4)
	n = u.PeekBatchCQE(cqeBuff)
	Equal(t, n, 4)

	for i := 0; i < 4; i++ {
		Equal(t, cqeBuff[i].UserData, uint64(i+4))
	}

	u.Advance(4)
	n = u.PeekBatchCQE(cqeBuff)
	Equal(t, n, 0)
}

func TestProbe(t *testing.T) {
	u, err := IOURing(8)
	MustNil(t, err)
	defer u.Close()

	probe, err := u.Probe()
	if errors.Is(err, syscall.EINVAL) {
		t.Skip("IORING_REGISTER_PROBE not supported")
	}
	MustNil(t, err)

	Assert(t, probe.lastOp != 0)
}

func TestCQSize(t *testing.T) {
	u, err := IOURing(8, CQSize(64))
	MustNil(t, err)
	Equal(t, u.Params.cqEntries, uint32(64))

	err = u.Close()
	MustNil(t, err)

	_, err = IOURing(4, CQSize(0))
	Assert(t, err != nil)
}

func makeV(f *os.File, vSZ int64) ([][]byte, error) {
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	bytes := stat.Size()
	blocks := int(math.Ceil(float64(bytes) / float64(vSZ)))

	buffs := make([][]byte, 0, blocks)
	for bytes != 0 {
		bytesToRead := bytes
		if bytesToRead > vSZ {
			bytesToRead = vSZ
		}

		buffs = append(buffs, make([]byte, bytesToRead))
		bytes -= bytesToRead
	}

	return buffs, nil
}

func vToString(v [][]byte) (str string) {
	for _, vector := range v {
		str += string(vector)
	}
	return
}

func queueSQEs(u *URing, count, offset int) (err error) {
	for i := 0; i < count; i++ {
		err = u.Queue(Nop(), 0, uint64(i+offset))
		if err != nil {
			return
		}
	}
	_, err = u.Submit()
	return
}
