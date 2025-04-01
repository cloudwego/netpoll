/*
 * Copyright 2025 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package runner

import (
	"context"
	"os"
	"strconv"

	bgopool "github.com/bytedance/gopkg/util/gopool"
	cgopool "github.com/cloudwego/gopkg/concurrency/gopool"
)

// RunTask runs the `f` in background, and `ctx` is optional.
// `ctx` is used to pass to underlying implementation
var RunTask func(ctx context.Context, f func())

func goRunTask(ctx context.Context, f func()) {
	go f()
}

func init() {
	// netpoll uses github.com/bytedance/gopkg/util/gopool by default
	// if the env is set, change it to cloudwego/gopkg
	// for most users, using the 'go' keyword directly is more suitable.
	if yes, _ := strconv.ParseBool(os.Getenv("USE_CLOUDWEGO_GOPOOL")); yes {
		RunTask = cgopool.CtxGo
	} else {
		RunTask = bgopool.CtxGo
	}
}

// UseGoRunTask updates RunTask with goRunTask which creates
// a new goroutine for the given func, basically `go f()`
func UseGoRunTask() {
	RunTask = goRunTask
}
