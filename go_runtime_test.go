package netpoll

import (
	"fmt"
	"testing"
)

/*	    === make copy ===
		size := 1024
		from := make([]byte, size)
		to := make([]byte, size)
		copy(to, from)
=>
        LEAQ    type:uint8(SB), AX
        MOVL    $1024, BX
        MOVQ    BX, CX
        PCDATA  $1, $0
        NOP
        CALL    runtime.makeslice(SB)
        MOVL    $1024, BX
        MOVQ    BX, CX
        MOVQ    AX, DI
        LEAQ    type:uint8(SB), AX
        CALL    runtime.makeslicecopy(SB)
*/

/*	    === make clear copy ===
		size := 1024
		from := make([]byte, size)
		var to []byte
		if size%2 == 0 {
			to = make([]byte, size)
		}
		copy(to[4:], from)
=>
        LEAQ    type:uint8(SB), AX
        MOVL    $1024, BX
        MOVQ    BX, CX
        PCDATA  $1, $0
        NOP
        CALL    runtime.makeslice(SB)
        MOVQ    AX, main..autotmp_14+24(SP)
        MOVL    $1024, BX
        MOVQ    BX, CX
        LEAQ    type:uint8(SB), AX
        PCDATA  $1, $1
        CALL    runtime.makeslice(SB)
        ADDQ    $4, AX
        MOVQ    main..autotmp_14+24(SP), BX
        CMPQ    BX, AX
        JEQ     main_main_pc86
        MOVL    $1020, CX
        PCDATA  $1, $0
        CALL    runtime.memmove(SB)
*/

/*	    === slice copy ===
		size := 1024
		from := make([]byte, size)
		to := make([]byte, size+5)
		copy(to[4:], from)
=>
        LEAQ    type:uint8(SB), AX
        MOVL    $1024, BX
        MOVQ    BX, CX
        PCDATA  $1, $0
        NOP
        CALL    runtime.makeslice(SB)
        MOVQ    AX, main..autotmp_15+24(SP)
        MOVL    $1029, BX
        MOVQ    BX, CX
        LEAQ    type:uint8(SB), AX
        PCDATA  $1, $1
        CALL    runtime.makeslice(SB)
        ADDQ    $4, AX
        MOVQ    main..autotmp_15+24(SP), BX
        CMPQ    BX, AX
        JEQ     main_main_pc86
        MOVL    $1024, CX
        PCDATA  $1, $0
        CALL    runtime.memmove(SB)
*/

/*	    === append copy ===
		size := 1024
		from := make([]byte, size)
		to := append([]byte(nil), from...)
		_ = to
=>
        LEAQ    type:uint8(SB), AX
        MOVL    $1024, BX
        MOVQ    BX, CX
        PCDATA  $1, $0
        NOP
        CALL    runtime.makeslice(SB)
        MOVQ    AX, main..autotmp_14+40(SP)
        MOVL    $1024, BX
        XORL    CX, CX
        MOVQ    BX, DI
        LEAQ    type:uint8(SB), SI
        XORL    AX, AX
        PCDATA  $1, $1
        NOP
        CALL    runtime.growslice(SB)
        MOVQ    main..autotmp_14+40(SP), BX
        MOVL    $1024, CX
        PCDATA  $1, $0
        CALL    runtime.memmove(SB)
*/

func newTestBytes(size int) []byte {
	buf := make([]byte, size)
	for i := 0; i < size; i++ {
		buf[i] = 'a' + byte(i%26)
	}
	return buf
}

func BenchmarkCopy(b *testing.B) {
	const (
		makeCopy         = "make-copy"
		makeClearCopy    = "make-clear-copy"
		sliceCopy        = "slice-copy"
		smallToLargeCopy = "small-to-large-copy"
		appendCopy       = "append-copy"
		foreachCopy      = "foreach-copy"
	)
	sizes := []int{
		32,
		256,
		//1000,
		//4000, 10240, 102400,
	}
	testTypes := []string{makeCopy, makeClearCopy, sliceCopy, smallToLargeCopy, appendCopy, foreachCopy}
	for _, size := range sizes {
		for _, kind := range testTypes {
			b.Run(fmt.Sprintf("size=%d,kind=%v", size, kind), func(b *testing.B) {
				from := newTestBytes(size)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var to []byte
					switch kind {
					case makeCopy:
						to = make([]byte, size)
						copy(to, from)
					case makeClearCopy:
						if size > 0 { // always true but treat compiler not optimise to makeslicecopy
							to = make([]byte, size)
						}
						copy(to, from)
					case sliceCopy:
						to = make([]byte, size+4)
						copy(to[4:], from)
					case smallToLargeCopy:
						to = make([]byte, size+1)
						copy(to, from)
					case appendCopy:
						to = append([]byte(nil), from...)
					case foreachCopy:
						to = make([]byte, size)
						for idx := 0; idx < len(from); idx++ {
							to[idx] = from[idx]
						}
					}
				}
			})
		}
	}
}
