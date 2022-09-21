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

package main

import (
	"fmt"
	"log"
	"os"

	. "github.com/cloudwego/netpoll/uring"
)

const BLOCK_SZ = 1024

type fileInfo struct {
	fd      uintptr
	fileSZ  int64
	buffs   [][]byte
	readvOp *ReadVOp /* Referred by readv/writev */
}

var fi fileInfo

/*
* Returns the size of the file whose open file descriptor is passed in.
* Properly handles regular file and block devices as well. Pretty.
* */
func getFileSize(file *os.File) int64 {
	stat, err := file.Stat()
	MustNil(err)

	return stat.Size()
}

/*
 * Output a string of characters of len length to stdout.
 * We use buffered output here to be efficient,
 * since we need to output character-by-character.
 * */
func outputToConsole(buff []byte) {
	fmt.Printf("%s", string(buff))
}

/*
 * Wait for a completion to be available, fetch the data from
 * the readv operation and print it to the console.
 * */
func getCompletionAndPrint(u *URing) (err error) {
	cqe, err := u.WaitCQE()
	MustNil(err)
	if cqe.Res < 0 {
		fmt.Printf("Async readv failed.\n")
	}

	blocks := int(fi.fileSZ) / BLOCK_SZ
	if fi.fileSZ%BLOCK_SZ != 0 {
		blocks++
	}
	for i := 0; i < blocks; i++ {
		outputToConsole(fi.buffs[i])
	}

	u.CQESeen()

	return nil
}

/*
 * Submit the readv request via liburing
 * */
func submitReadRequest(u *URing, fileName string) (err error) {
	file, err := os.Open(fileName)
	MustNil(err)

	fileSZ := getFileSize(file)
	bytesRemaining := fileSZ

	blocks := int(fileSZ / BLOCK_SZ)
	if fileSZ%BLOCK_SZ != 0 {
		blocks++
	}

	buffs := make([][]byte, 0, blocks)

	/*
	 * For each block of the file we need to read, we allocate an iovec struct
	 * which is indexed into the iovecs array. This array is passed in as part
	 * of the submission. If you don't understand this, then you need to look
	 * up how the readv() and writev() system calls work.
	 * */
	for bytesRemaining != 0 {
		bytesToRead := bytesRemaining

		if bytesToRead > BLOCK_SZ {
			bytesToRead = BLOCK_SZ
		}

		buffs = append(buffs, make([]byte, bytesToRead))
		bytesRemaining -= bytesToRead
	}

	fi := &fileInfo{
		fd:      file.Fd(),
		fileSZ:  fileSZ,
		buffs:   buffs,
		readvOp: ReadV(file.Fd(), buffs, 0),
	}
	/* Setup a readv operation, user data */
	err = u.Queue(fi.readvOp, 0, uint64(fi.fd))
	/* Finally, submit the request */
	u.Submit()
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s [file name] <[file name] ...>\n",
			os.Args[0])
		return
	}

	/* Initialize io_uring */
	u, err := IOURing(8)
	MustNil(err)
	/* Call the clean-up function. */
	defer u.Close()

	for _, fileName := range os.Args[1:] {
		err := submitReadRequest(u, fileName)
		MustNil(err)

		getCompletionAndPrint(u)
	}
}

func MustNil(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
