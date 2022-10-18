//+build !race

package netpoll

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/bytedance/gopkg/lang/mcache"
)

func TestPCacheSingleP(t *testing.T) {
	procs := runtime.GOMAXPROCS(0)
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(procs)
	Equal(t, runtime.GOMAXPROCS(0), 1)

	pc := newPCache()
	buf := pc.Malloc(1024, 1025)
	Equal(t, len(buf), 1024)
	Equal(t, cap(buf), 1025)
	pc.Free(buf)
	Equal(t, len(pc.active[0][calcCapIndex(1025)]), 1)

	buf = pc.Malloc(1024, 1024)
	Equal(t, len(buf), 1024)
	Equal(t, cap(buf), 1024)
	pc.Free(buf)
	Equal(t, len(pc.active[0][calcCapIndex(1024)]), 1)
	Equal(t, len(pc.active[0][calcCapIndex(1025)]), 1)
}

func TestPCacheMultiP(t *testing.T) {
	pc := newLimitedPCache(1024) // 1KB
	size := 50
	testdata := make([]byte, size)
	for i := 0; i < len(testdata); i++ {
		testdata[i] = 'a' + byte(i%26)
	}
	procs := runtime.GOMAXPROCS(0)
	var wg sync.WaitGroup
	for i := 0; i < procs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				buf := pc.Malloc(size, size)
				copy(buf, testdata)
				Equal(t, len(buf), size)
				Equal(t, cap(buf), size)
				Equal(t, string(buf), string(testdata))
				runtime.Gosched()
				pc.Free(buf)
			}
		}()
	}
	wg.Wait()
}

func TestPCacheGC(t *testing.T) {
	procs := runtime.GOMAXPROCS(0)
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(procs)
	Equal(t, runtime.GOMAXPROCS(0), 1)

	pc := newLimitedPCache(1024)
	buf1 := pc.Malloc(1024, 1024)
	buf2 := pc.Malloc(1024, 1024)
	Equal(t, len(buf1), 1024)
	Equal(t, cap(buf1), 1024)
	Equal(t, len(buf2), 1024)
	Equal(t, cap(buf2), 1024)
	pc.Free(buf1)
	pc.Free(buf2)
	Equal(t, len(pc.active[0][calcCapIndex(1024)]), 1)
	Equal(t, len(pc.inactive[0][calcCapIndex(1024)]), 1)

	for i := 0; i < defaultPCacheCleanCycles; i++ {
		runtime.GC()
	}
	Equal(t, len(pc.active[0][calcCapIndex(1024)]), 1)
	Equal(t, len(pc.inactive[0][calcCapIndex(1024)]), 0)
	buf1 = pc.Malloc(1024, 1024)
	buf2 = pc.Malloc(1024, 1024)
	pc.Free(buf1)
	pc.Free(buf2)
	Equal(t, len(pc.active[0][calcCapIndex(1024)]), 1)
	Equal(t, len(pc.inactive[0][calcCapIndex(1024)]), 1)
	for i := 0; i < defaultPCacheCleanCycles; i++ {
		runtime.GC()
	}
	Equal(t, len(pc.active[0][calcCapIndex(1024)]), 1)
	Equal(t, len(pc.inactive[0][calcCapIndex(1024)]), 0)
}

var benchSizes = []int{
	1024,
	1024 * 8, 1024 * 64, // small size
	1024 * 1024, 1024 * 1024 * 8, // large size
} // unit: Bytes

func BenchmarkPCache(b *testing.B) {
	for _, size := range benchSizes {
		pc := newLimitedPCache(1024 * 1024 * 1) // 1MB
		b.Run(fmt.Sprintf("Malloc-%d", size), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					buf := pc.Malloc(size)
					pc.Free(buf)
				}
			})
		})
		d := make([]int, len(pc.active))
		for pid := 0; pid < len(pc.active); pid++ {
			for cidx := 0; cidx < len(pc.active[pid]); cidx++ {
				for i := 0; i < len(pc.active[pid][cidx]); i++ {
					d[pid] += cap(pc.active[pid][cidx][i])
				}
			}
		}
		b.Logf("pcache-%d active distribution: %v", size, d)
	}
}

func BenchmarkMCache(b *testing.B) {
	for _, size := range benchSizes {
		b.Run(fmt.Sprintf("Malloc-%d", size), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					buf := mcache.Malloc(size)
					mcache.Free(buf)
				}
			})
		})
	}
}
