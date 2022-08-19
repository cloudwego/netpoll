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
	"io"
	"io/ioutil"
	"testing"
)

func TestZCReader(t *testing.T) {
	reader := &MockIOReadWriter{
		read: func(p []byte) (n int, err error) {
			return len(p), nil
		},
	}
	r := newZCReader(reader)

	p, err := r.Next(block8k)
	MustNil(t, err)
	Equal(t, len(p), block8k)
	Equal(t, r.buf.Len(), 0)

	p, err = r.Peek(block4k)
	MustNil(t, err)
	Equal(t, len(p), block4k)
	Equal(t, r.buf.Len(), block4k)

	err = r.Skip(block4k)
	MustNil(t, err)
	Equal(t, r.buf.Len(), 0)

	err = r.Release()
	MustNil(t, err)
}

func TestZCWriter(t *testing.T) {
	writer := &MockIOReadWriter{
		write: func(p []byte) (n int, err error) {
			return len(p), nil
		},
	}
	w := newZCWriter(writer)

	p, err := w.Malloc(block1k)
	MustNil(t, err)
	Equal(t, len(p), block1k)
	Equal(t, w.buf.Len(), 0)

	err = w.Flush()
	MustNil(t, err)
	Equal(t, w.buf.Len(), 0)

	p, err = w.Malloc(block2k)
	MustNil(t, err)
	Equal(t, len(p), block2k)
	Equal(t, w.buf.Len(), 0)

	err = w.buf.Flush()
	MustNil(t, err)
	Equal(t, w.buf.Len(), block2k)

	err = w.Flush()
	MustNil(t, err)
	Equal(t, w.buf.Len(), 0)
}

func TestZCEOF(t *testing.T) {
	reader := &MockIOReadWriter{
		read: func(p []byte) (n int, err error) {
			return 0, io.EOF
		},
	}
	r := newZCReader(reader)

	_, err := r.Next(block8k)
	MustTrue(t, errors.Is(err, ErrEOF))
}

type MockIOReadWriter struct {
	read  func(p []byte) (n int, err error)
	write func(p []byte) (n int, err error)
}

func (rw *MockIOReadWriter) Read(p []byte) (n int, err error) {
	if rw.read != nil {
		return rw.read(p)
	}
	return
}

func (rw *MockIOReadWriter) Write(p []byte) (n int, err error) {
	if rw.write != nil {
		return rw.write(p)
	}
	return
}

func TestIOReadWriter(t *testing.T) {
	buf := NewLinkBuffer(block1k)
	reader, writer := newIOReader(buf), newIOWriter(buf)
	msg := []byte("hello world")
	n, err := writer.Write(msg)
	MustNil(t, err)
	Equal(t, n, len(msg))

	p := make([]byte, block1k)
	n, err = reader.Read(p)
	MustNil(t, err)
	Equal(t, n, len(msg))
}

func TestIOReadWriter2(t *testing.T) {
	buf := NewLinkBuffer(block1k)
	reader, writer := newIOReader(buf), newIOWriter(buf)
	msg := []byte("hello world")
	n, err := writer.Write(msg)
	MustNil(t, err)
	Equal(t, n, len(msg))

	p, err := ioutil.ReadAll(reader)
	MustNil(t, err)
	Equal(t, len(p), len(msg))
}
