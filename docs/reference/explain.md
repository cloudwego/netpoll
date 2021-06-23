# DATA RACE EXPLAIN
`Netpoll` declare different files by `//+build !race` and `//+build race` to avoid `DATA RACE` detection in some code.

The reason is that the `epoll` uses `unsafe.Pointer` to access the struct pointer, in order
 to improve performance. This operation is beyond the detection range of the `race detector`,
 so it is mistaken for data race, but not code bug actually.
