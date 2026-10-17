package main

import (
	"bufio"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"time"
)

// emit writes coloured lines: first prefill bytes as fast as it can (to fill
// scrollback), then rate bytes a second, open loop: it keeps its pace
// whether or not anything downstream keeps up. With --stamp, each tick's
// first line carries the time it was written (see stampPrefix), for
// end-to-end latency.
func emit(args []string) error {
	fs := flag.NewFlagSet("emit", flag.ContinueOnError)
	rate := fs.Int("rate", 100<<10, "bytes a second after the prefill")
	prefill := fs.Int("prefill", 0, "bytes written at once first")
	stamp := fs.Bool("stamp", false, "stamp each tick's first line with the time")
	osc := fs.String("osc", "", "also write OSC sequences: title, pwd, status, bell, clipboard or notify")
	oscRate := fs.Int("osc-rate", 0, "OSC sequences a second, each with a new value")
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
	oscPerTick := *oscRate * int(tick) / int(time.Second)
	if *oscRate > 0 && oscPerTick == 0 {
		oscPerTick = 1 // under 100 a second: one a tick, the rate rounds up
	}
	seq := 0
	next := time.Now()
	for {
		for i := 0; *osc != "" && i < oscPerTick; i++ {
			seq++
			writeOSC(w, *osc, seq)
		}
		if *stamp {
			fmt.Fprintf(w, "%s%019d;", stampPrefix, time.Now().UnixNano())
		}
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

// writeOSC writes one sequence of kind, its value changing with n so that
// nothing downstream can skip it as unchanged.
func writeOSC(w *bufio.Writer, kind string, n int) {
	switch kind {
	case "title":
		fmt.Fprintf(w, "\x1b]2;title %d\x07", n)
	case "pwd":
		fmt.Fprintf(w, "\x1b]7;file://localhost/tmp/d%d\x07", n)
	case "status":
		fmt.Fprintf(w, "\x1b]7501;state=%s:id=job%d:app=bench\x1b\\", [...]string{"working", "idle", "blocked"}[n%3], n%50)
	case "bell":
		w.WriteByte(7)
	case "clipboard":
		fmt.Fprintf(w, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(fmt.Sprint("copied ", n))))
	case "notify":
		fmt.Fprintf(w, "\x1b]9;note %d\x07", n)
	}
}
