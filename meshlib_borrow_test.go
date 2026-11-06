package netpoll

import (
	"bytes"
	"testing"
)

func TestMeshlibBorrowConsumedMessagePreservesNodesAndBoundary(t *testing.T) {
	b := NewLinkBuffer()
	defer b.Close()
	first := []byte("first-message")
	second := []byte("next-message")
	_, _ = b.WriteBinary(first)
	_ = b.Flush()
	mark := b.MarkRead()
	if err := b.Skip(len(first)); err != nil {
		t.Fatal(err)
	}
	_, _ = b.WriteBinary(second)
	_ = b.Flush()
	var scratch [4][]byte
	parts := b.ConsumedSlices(mark, scratch[:0])
	if got := b.ConsumedLen(mark); got != len(first) {
		t.Fatalf("consumed %d bytes, want %d", got, len(first))
	}
	if len(parts) != 1 || !bytes.Equal(parts[0], first) {
		t.Fatal(parts)
	}
	firstView := parts[0]
	again := b.ConsumedSlices(mark, scratch[:0])
	if &again[0][0] != &firstView[0] {
		t.Fatal("copied payload")
	}
	remaining, err := b.Next(len(second))
	if err != nil || !bytes.Equal(remaining, second) {
		t.Fatal("consumed next message", err)
	}
	if got := b.ConsumedLen(mark); got != len(first)+len(second) {
		t.Fatalf("consumed %d bytes across nodes", got)
	}
}

func TestMeshlibReadMarksSurviveCopyReadsUntilRelease(t *testing.T) {
	b := NewLinkBuffer()
	defer b.Close()
	for _, text := range []string{"first-block", "middle-block", "last-block"} {
		_, _ = b.WriteBinary([]byte(text))
		_ = b.Flush()
	}
	first := b.MarkRead()
	if n := b.readCopy(make([]byte, len("first-block"))); n != len("first-block") {
		t.Fatal(n)
	}
	second := b.MarkRead()
	if n := b.readCopy(make([]byte, len("middle-block")+1)); n != len("middle-block")+1 {
		t.Fatal(n)
	}
	for _, test := range []struct {
		mark ReadMark
		want string
	}{{first, "first-blockmiddle-blockl"}, {second, "middle-blockl"}} {
		if got := b.ConsumedLen(test.mark); got != len(test.want) {
			t.Fatalf("consumed=%d want=%d", got, len(test.want))
		}
		parts := b.ConsumedSlices(test.mark, nil)
		if got := string(bytes.Join(parts, nil)); got != test.want {
			t.Fatalf("borrowed=%q want=%q", got, test.want)
		}
	}
	if err := b.Release(); err != nil {
		t.Fatal(err)
	}
	if b.readMarked || b.head != b.read {
		t.Fatal("Release retained a mark or consumed nodes")
	}
}

func TestMeshlibCopyReadsStillReleaseNodesWithoutMarks(t *testing.T) {
	b := NewLinkBuffer()
	defer b.Close()
	for _, text := range []string{"first-block", "last-block"} {
		_, _ = b.WriteBinary([]byte(text))
		_ = b.Flush()
	}
	if n := b.readCopy(make([]byte, len("first-block")+1)); n != len("first-block")+1 {
		t.Fatal(n)
	}
	if b.head != b.read {
		t.Fatal("unmarked copy read retained consumed nodes")
	}
}
