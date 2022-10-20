package netpoll

import (
	"log"
	"math/bits"
	"reflect"
	"runtime"
	"unsafe"
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
	debug                    = false
	defaultPCacheMaxSize     = 42 // 2^42=4 TB
	defaultPCacheBlockSize   = 1024 * 1024 * 16
	defaultPCacheCleanCycles = 3
)

type pcache struct {
	arena          []byte                           // fixed size continuous memory
	arenaStart     uintptr                          // arena address start
	arenaEnd       uintptr                          // arena address end
	activeBlocks   [][]byte                         // [pid][]byte
	inactiveBlocks [][]byte                         // [pid][]byte
	active         [][defaultPCacheMaxSize][][]byte // [pid][cap_idx][idx]stack
	inactive       [][defaultPCacheMaxSize][][]byte // [pid][cap_idx][idx]stack
	ref            *pcacheRef                       // for gc trigger
}

type pcacheRef struct {
	pc *pcache
	gc int
}

func gcRefHandler(ref *pcacheRef) {
	defer runtime.SetFinalizer(ref, gcRefHandler)
	ref.gc++
	if ref.gc < defaultPCacheCleanCycles {
		return
	}
	ref.gc = 0

	// trigger handler
	pid := procPin()
	var buf [][]byte
	var l, c, released int
	for i := 0; i < defaultPCacheMaxSize; i++ {
		l = len(ref.pc.inactive[pid][i])
		if l == 0 {
			continue
		}
		c = 1 << i
		buf = make([][]byte, l/2, l)
		copy(buf, ref.pc.inactive[pid][i][:l/2]) // the first l/2 items are more inactive
		ref.pc.inactive[pid][i] = buf
		released += c * len(buf)
	}
	procUnpin()
	if debug && released > 0 {
		log.Printf("PCACHE: P[%d] release: %d bytes", pid, released)
	}
}

func newPCache() *pcache {
	return newLimitedPCache(defaultPCacheBlockSize * runtime.GOMAXPROCS(0))
}

func newLimitedPCache(size int) *pcache {
	procs := runtime.GOMAXPROCS(0)
	sizePerP := size / procs
	pc := &pcache{
		activeBlocks:   make([][]byte, procs),
		inactiveBlocks: make([][]byte, procs),
		active:         make([][defaultPCacheMaxSize][][]byte, procs),
		inactive:       make([][defaultPCacheMaxSize][][]byte, procs),
	}

	// init arena
	pc.arena = make([]byte, size)
	pc.arenaStart = uintptr(unsafe.Pointer(&pc.arena[0]))
	pc.arenaEnd = uintptr(unsafe.Pointer(&pc.arena[len(pc.arena)-1]))
	for i := 0; i < procs; i++ {
		pc.activeBlocks[i] = pc.arena[i*sizePerP : (i+1)*sizePerP]
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
	clen := 1 << cidx

	pid := procPin()
	l := len(p.active[pid][cidx])
	if l > 0 {
		data = p.active[pid][cidx][l-1][:size:capacity]
		p.active[pid][cidx] = p.active[pid][cidx][:l-1]
		procUnpin()
		if debug {
			log.Printf("PCACHE: P[%d] reuse active %d bytes, addr %d", pid, clen, uintptr(unsafe.Pointer(&data[:1][0])))
		}
		return data
	}

	l = len(p.inactive[pid][cidx])
	if l > 0 {
		data = p.inactive[pid][cidx][l-1][:size:capacity]
		p.inactive[pid][cidx] = p.inactive[pid][cidx][:l-1]
		procUnpin()
		if debug {
			log.Printf("PCACHE: P[%d] reuse inactive %d bytes, addr %d", pid, clen, uintptr(unsafe.Pointer(&data[:1][0])))
		}
		return data
	}

	if clen <= len(p.activeBlocks[pid]) {
		data = p.activeBlocks[pid][:size:capacity]
		p.activeBlocks[pid] = p.activeBlocks[pid][clen:]
		procUnpin()
		if debug {
			log.Printf("PCACHE: P[%d] malloc from activeBlock %d bytes, addr %d", pid, clen, uintptr(unsafe.Pointer(&data[:1][0])))
		}
		return data
	}

	if clen > len(p.inactiveBlocks[pid]) {
		if clen < defaultPCacheBlockSize {
			p.inactiveBlocks[pid] = make([]byte, defaultPCacheBlockSize)
		} else {
			p.inactiveBlocks[pid] = make([]byte, clen)
		}
	}
	data = p.inactiveBlocks[pid][:size:capacity]
	p.inactiveBlocks[pid] = p.inactiveBlocks[pid][clen:]
	procUnpin()
	if debug {
		log.Printf("PCACHE: P[%d] malloc from inactiveBlock %d bytes, addr %d", pid, clen, uintptr(unsafe.Pointer(&data[:1][0])))
	}
	return data
}

func (p *pcache) Free(data []byte) {
	capacity := cap(data)
	if capacity == 0 {
		return
	}
	cidx := calcCapIndex(capacity)
	clen := 1 << cidx
	dp := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	addr := dp.Data
	data = data[:0]
	dp.Cap = clen

	pid := procPin()
	if addr >= p.arenaStart && addr <= p.arenaEnd {
		p.active[pid][cidx] = append(p.active[pid][cidx], data)
		procUnpin()
		if debug {
			log.Printf("PCACHE: P[%d] free active %d bytes, addr %d", pid, clen, addr)
		}
		return
	}

	p.inactive[pid][cidx] = append(p.inactive[pid][cidx], data)
	procUnpin()
	if debug {
		log.Printf("PCACHE: P[%d] free inactive %d bytes, addr %d", pid, clen, addr)
	}
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
