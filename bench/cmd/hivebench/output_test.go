package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestScanStampsAcrossFrames(t *testing.T) {
	stream := []byte(fmt.Sprintf("noise\r\n%s%019d;line\r\nmore %s%019d;x", stampPrefix, int64(1700000000123456789), stampPrefix, int64(42)))
	want := []int64{1700000000123456789, 42}
	// Every split point of the stream into two frames finds both, once.
	for cut := 0; cut <= len(stream); cut++ {
		a, tail := scanStamps(nil, stream[:cut])
		b, _ := scanStamps(tail, stream[cut:])
		if got := append(a, b...); !reflect.DeepEqual(got, want) {
			t.Fatalf("cut at %d: %v", cut, got)
		}
	}
	// Byte by byte.
	var got []int64
	var tail []byte
	for i := range stream {
		var s []int64
		s, tail = scanStamps(tail, stream[i:i+1])
		got = append(got, s...)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("byte by byte: %v", got)
	}
}

func TestSlopeIsPerHour(t *testing.T) {
	// 1 MB a minute is 60 MB an hour; flat is 0.
	if s := slope([]float64{0, 1, 2, 3}, []float64{10, 11, 12, 13}); s < 59.99 || s > 60.01 {
		t.Fatalf("slope = %v", s)
	}
	if s := slope([]float64{0, 1, 2}, []float64{5, 5, 5}); s != 0 {
		t.Fatalf("flat slope = %v", s)
	}
}
