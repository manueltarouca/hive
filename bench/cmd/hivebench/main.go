// Command hivebench measures hive: it finds where the server stops keeping
// up (the knee) and what it's doing there. Every scenario ramps one
// dimension and stops after two degraded steps. See bench/README.md.
//
//	hivebench emit [--rate BYTES/s] [--prefill BYTES]   load generator (a pane's program)
//	hivebench flush [--secs N] [--out DIR]              saves × panes × scrollback
//	hivebench output [--panes N] [--secs N]             output rate → latency to a client
//	hivebench ptyread [--max N] [--rate B]              busy panes → pty read cost, alone
//	hivebench storm [--kinds K] [--panes N]             OSC sequences a second → events, API latency, memory
//	hivebench resize [--rate B]                         resizes a second → latency, reflow, final size
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: hivebench emit|flush|output|ptyread|storm|resize [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "emit":
		err = emit(os.Args[2:])
	case "flush":
		err = flushProbe(os.Args[2:])
	case "output":
		err = outputProbe(os.Args[2:])
	case "ptyread":
		err = ptyProbe(os.Args[2:])
	case "storm":
		err = stormProbe(os.Args[2:])
	case "resize":
		err = resizeProbe(os.Args[2:])
	default:
		err = fmt.Errorf("unknown scenario %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hivebench:", err)
		os.Exit(1)
	}
}
