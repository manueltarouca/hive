// Package id makes hive's object IDs: a kind, a colon, and a UUIDv7, such as
// block:01a10d99-0c06-7795-bf67-798ce4c36a33. IDs sort by creation time,
// never change, and are never reused.
package id

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	Session = "session"
	Window  = "window"
	Block   = "block"
	Split   = "split"
	Client  = "client"
)

var (
	mu      sync.Mutex
	lastMS  int64
	counter uint16
)

// New returns a new ID of the given kind.
func New(kind string) string {
	mu.Lock()
	ms := time.Now().UnixMilli()
	if ms <= lastMS {
		// Same or earlier millisecond: keep the timestamp and count up, so
		// IDs from one process are strictly increasing.
		ms = lastMS
		counter++
	} else {
		counter = 0
	}
	lastMS = ms
	seq := counter
	mu.Unlock()

	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(ms)<<16)
	_, _ = rand.Read(b[8:])
	// 12 bits of sequence after the version nibble.
	b[6] = 0x70 | byte(seq>>8)&0x0f
	b[7] = byte(seq)
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%s:%x-%x-%x-%x-%x", kind, b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// KindOf returns the kind of an ID, or "" if it has none.
func KindOf(s string) string {
	k, _, ok := strings.Cut(s, ":")
	if !ok {
		return ""
	}
	return k
}

// Short returns the last six characters of an ID, as listings print them.
func Short(s string) string {
	if len(s) <= 6 {
		return s
	}
	return s[len(s)-6:]
}
