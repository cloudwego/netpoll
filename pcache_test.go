package netpoll

import (
	"runtime"
	"testing"
)

func TestPCache(t *testing.T) {
	procs := runtime.GOMAXPROCS(0)
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(procs)
	Equal(t, runtime.GOMAXPROCS(0), 1)

	pc := newPCache()
	buf := pc.Malloc(1024, 1025)
	Equal(t, len(buf), 1024)
	Equal(t, cap(buf), 2048)
	pc.Free(buf)
	Equal(t, len(pc.active[0][calcCapIndex(1025)]), 1)

	buf = pc.Malloc(1024, 1024)
	Equal(t, len(buf), 1024)
	Equal(t, cap(buf), 1024)
	pc.Free(buf)
	Equal(t, len(pc.active[0][calcCapIndex(1025)]), 1)
	Equal(t, len(pc.active[0][calcCapIndex(1024)]), 1)
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
