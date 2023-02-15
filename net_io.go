package netpoll

import "syscall"

// return value:
// - n: n == 0 but err == nil, retry syscall
// - err: if not nil, connection should be closed.
func ioread(fd int, bs [][]byte, ivs []syscall.Iovec) (n int, err error) {
	n, err = readv(fd, bs, ivs)
	if n == 0 && err == nil { // means EOF
		return 0, Exception(ErrEOF, "")
	}
	if err == syscall.EINTR || err == syscall.EAGAIN {
		return 0, nil
	}
	return n, err
}

// return value:
// - n: n == 0 but err == nil, retry syscall
// - err: if not nil, connection should be closed.
func iosend(fd int, bs [][]byte, ivs []syscall.Iovec, zerocopy bool) (n int, err error) {
	n, err = sendmsg(fd, bs, ivs, zerocopy)
	if err == syscall.EAGAIN {
		return 0, nil
	}
	return n, err
}
