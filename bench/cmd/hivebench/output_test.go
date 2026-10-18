package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
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

// Each measurement starts from zero, or the soak's intervals pile up.
func TestReaderStartResets(t *testing.T) {
	r := &reader{tails: map[string][]byte{}}
	r.start()
	r.lat, r.bytes, r.resyncs, r.frames = make([]time.Duration, 5), 10, 1, 3
	r.start()
	if len(r.lat) != 0 || r.bytes != 0 || r.resyncs != 0 || r.frames != 0 {
		t.Fatalf("after start: %d samples, %d bytes, %d resyncs, %d frames", len(r.lat), r.bytes, r.resyncs, r.frames)
	}
}

// Two runs that end in the same second keep both results.
func TestWriteResultNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	a, err := writeResult(dir, "soak", map[string]int{"run": 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := writeResult(dir, "soak", map[string]int{"run": 2})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("both runs wrote %s", a)
	}
	if got, _ := os.ReadFile(a); !strings.Contains(string(got), `"run": 1`) {
		t.Fatalf("first result overwritten: %s", got)
	}
}

func TestLinuxMetaParsing(t *testing.T) {
	if got := procLoadavg("0.52 0.58 0.59 1/389 12345\n"); got != "0.52 0.58 0.59" {
		t.Errorf("procLoadavg = %q", got)
	}
	if got := memTotalGB("MemTotal:       16303840 kB\nMemFree:  1 kB\n"); got < 15.5 || got > 15.6 {
		t.Errorf("memTotalGB = %v", got)
	}
	if got := osRelease("NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\n"); got != "Ubuntu 24.04.1 LTS" {
		t.Errorf("osRelease = %q", got)
	}
	if got := lscpuModel("Architecture: aarch64\nModel name:  Neoverse-V1\n"); got != "Neoverse-V1" {
		t.Errorf("lscpuModel = %q", got)
	}
}
