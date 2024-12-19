package gopool

import (
	"context"
	"runtime"
	"time"
)

type task struct {
	ctx context.Context
	f   func()
}

type GoPool struct {
	tasks chan task

	tempWorkerTimeout time.Duration
}

// defaultTaskChanBufferSize is the size of buffered channel for task.
//
// The value should be large enough for the channel to serve as a task queue.
// If the task channel is full,
// the code will fall back to a regular call with a new goroutine.
// If the task channel is NOT zero,
// the code will try to create a new goroutine
const defaultTaskChanBufferSize = 10000

// defaultTemporaryWorkerTimeout is how long a temporary worker works before it retires
const defaultTemporaryWorkerTimeout = 100 * time.Millisecond

var defaultGoPool = newGoPool(defaultTaskChanBufferSize, defaultTemporaryWorkerTimeout)

func newGoPool(chansz int, workerTimeout time.Duration) *GoPool {
	return &GoPool{
		tasks:             make(chan task, chansz),
		tempWorkerTimeout: defaultTemporaryWorkerTimeout,
	}
}

func (p *GoPool) GoCtx(ctx context.Context, f func()) {
	select {
	case p.tasks <- task{ctx: ctx, f: f}:
		// queued, and we will check if it's picked up soon

	default:
		// full? creating a new goroutine
		go p.runTask(ctx, f)
		return
	}

	// likely p.tasks==0 which means a worker picks the task
	if len(p.tasks) > 0 {
		// if not, try schedule
		runtime.Gosched()
		// and then double check
		if len(p.tasks) > 0 {
			// all worker is busy?
			// hire a new and temporary worker for p.tempWorkerTimeout
			go p.runTemporaryWorker()
		}
	}
}

func (p *GoPool) runTask(ctx context.Context, f func()) {
	defer func(ctx context.Context) {
		// TODO:
	}(ctx)
	f()
}

func (p *GoPool) runTemporaryWorker() {
	timeout := time.After(p.tempWorkerTimeout)
	for {
		select {
		case t := <-p.tasks: // fastpath without checking timeout
			p.runTask(t.ctx, t.f)

		default:
			// no more task? wait
			select {
			case t := <-p.tasks:
				p.runTask(t.ctx, t.f)
			case <-timeout:
				// A worker might work overtime
				// because we only check timeout when there is no more task.
				return
			}
		}
	}
}

func (p *GoPool) runWorker() {
	for t := range p.tasks {
		p.runTask(t.ctx, t.f)
	}
}
