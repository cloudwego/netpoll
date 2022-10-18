package netpoll

// #include <stdlib.h>
import "C"
import (
	"unsafe"
)

const arenaSize = 4 << 30 // 4gb

func NewArena(size int) []byte {
	data := ((*[arenaSize]byte)(unsafe.Pointer(C.calloc(C.size_t(size), C.size_t(1)))))[:size:size]
	return data
}
