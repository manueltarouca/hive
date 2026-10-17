// Command hivebench measures hive: it finds where the server stops keeping
// up (the knee) and what it's doing there. Every scenario ramps one
// dimension and stops after two degraded steps. See bench/README.md.
//
//	hivebench emit [--rate BYTES/s] [--prefill BYTES]   load generator (a pane's program)
//	hivebench flush [--secs N] [--out DIR]              saves × panes × scrollback
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: hivebench emit|flush [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "emit":
		err = emit(os.Args[2:])
	case "flush":
		err = flushProbe(os.Args[2:])
	default:
		err = fmt.Errorf("unknown scenario %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hivebench:", err)
		os.Exit(1)
	}
}
