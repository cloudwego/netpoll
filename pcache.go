package netpoll

import (
	"math/bits"
	"runtime"
	_ "unsafe"
)

//go:linkname procPin runtime.procPin
func procPin() int

//go:linkname procUnpin runtime.procUnpin
func procUnpin() int

var globalPCache = newPCache()

func Malloc(size int, capacity ...int) []byte {
	return globalPCache.Malloc(size, capacity...)
}

func Free(buf []byte) {
	globalPCache.Free(buf)
}

const (
	defaultPCacheMaxSize     = 42               // 2^42=4 TB
	defaultPCacheLimitPerP   = 1024 * 1024 * 64 // 64 MB
	defaultPCacheCleanCycles = 3                // clean inactive every 3 gc cycles
)

type pcache struct {
	active      [][defaultPCacheMaxSize][][]byte // [pid][cap_idx][idx][]byte
	activeSize  []int64                          // [pid]int64, every P's active size
	activeLimit int64                            // active_limit_size = total_limit / GOMAXPROCS
	inactive    [][defaultPCacheMaxSize][][]byte // [pid][cap_idx][idx][]byte
	ref         *pcacheRef
}

type pcacheRef struct {
	pc *pcache
	gc int
}

func gcRefHandler(ref *pcacheRef) {
	ref.gc++
	// trigger handler every gc cycle
	if ref.gc >= defaultPCacheCleanCycles {
		ref.gc = 0
		pid := procPin()
		var l int
		for i := 0; i < defaultPCacheMaxSize; i++ {
			l = len(ref.pc.inactive[pid][i])
			if l == 0 {
				continue
			}
			ref.pc.inactive[pid][i] = ref.pc.inactive[pid][i][:l/2]
		}
		procUnpin()
	}
	runtime.SetFinalizer(ref, gcRefHandler)
}

func newPCache() *pcache {
	return newLimitedPCache(defaultPCacheLimitPerP)
}

func newLimitedPCache(limitPerP int64) *pcache {
	procs := runtime.GOMAXPROCS(0)
	pc := &pcache{
		active:      make([][defaultPCacheMaxSize][][]byte, procs),
		activeSize:  make([]int64, procs),
		inactive:    make([][defaultPCacheMaxSize][][]byte, procs),
		activeLimit: limitPerP,
	}
	pc.ref = &pcacheRef{pc: pc}
	runtime.SetFinalizer(pc.ref, gcRefHandler)
	pc.ref = nil // trigger gc
	return pc
}

func (p *pcache) Malloc(size int, _capacity ...int) (data []byte) {
	var capacity = size
	if len(_capacity) > 0 && _capacity[0] > size {
		capacity = _capacity[0]
	}
	cidx := calcCapIndex(capacity)
	capacity = 1 << cidx

	pid := procPin()
	if len(p.active[pid][cidx]) > 0 {
		data = p.active[pid][cidx][0]
		p.active[pid][cidx] = p.active[pid][cidx][1:]
		p.activeSize[pid] -= int64(capacity)
	} else if len(p.inactive[pid][cidx]) > 0 {
		data = p.inactive[pid][cidx][0]
		p.inactive[pid][cidx] = p.inactive[pid][cidx][1:]
	} else {
		data = make([]byte, size, capacity)
	}
	procUnpin()
	return data[:size]
}

func (p *pcache) Free(data []byte) {
	capacity := cap(data)
	cidx := calcCapIndex(capacity)
	data = data[:0]

	pid := procPin()
	// if out of active limit, put into inactive
	if p.activeLimit > 0 && p.activeSize[pid] >= p.activeLimit {
		p.inactive[pid][cidx] = append(p.inactive[pid][cidx], data)
	} else {
		p.activeSize[pid] += int64(capacity)
		p.active[pid][cidx] = append(p.active[pid][cidx], data)
	}
	procUnpin()
}

func calcCapIndex(size int) int {
	if size == 0 {
		return 0
	}
	if isPowerOfTwo(size) {
		return bsr(size)
	}
	return bsr(size) + 1
}

func bsr(x int) int {
	return bits.Len(uint(x)) - 1
}

func isPowerOfTwo(x int) bool {
	return (x & (-x)) == x
}
