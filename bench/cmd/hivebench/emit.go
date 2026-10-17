package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"time"
)

// emit writes coloured lines: first prefill bytes as fast as it can (to fill
// scrollback), then rate bytes a second, open loop: it keeps its pace
// whether or not anything downstream keeps up.
func emit(args []string) error {
	fs := flag.NewFlagSet("emit", flag.ContinueOnError)
	rate := fs.Int("rate", 100<<10, "bytes a second after the prefill")
	prefill := fs.Int("prefill", 0, "bytes written at once first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w := bufio.NewWriterSize(os.Stdout, 64<<10)
	n := 0
	line := func() int {
		n++
		k, _ := fmt.Fprintf(w, "\x1b[3%dm%08d\x1b[0m the quick brown fox jumps over the lazy dog \x1b[1mbold\x1b[0m %x\r\n", n%7+1, n, n*2654435761)
		return k
	}
	for written := 0; written < *prefill; {
		written += line()
	}
	w.Flush()
	const tick = 10 * time.Millisecond
	perTick := *rate * int(tick) / int(time.Second)
	next := time.Now()
	for {
		for written := 0; written < perTick; {
			written += line()
		}
		w.Flush()
		next = next.Add(tick)
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		}
	}
}
