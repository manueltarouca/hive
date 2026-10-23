#!/usr/bin/env python3
"""A Claude Code session as it looks mid-work: a long transcript with tool
calls, a diff and a list, then its input box and status bar. It's raw: each
key it gets is shown in the input box (arrows by name), so a test can read
what the app sent. Not a shell, so hive keeps it as a program."""
import os
import shutil
import signal
import sys
import textwrap
import tty

WORK = """● I'll look at how the cache is invalidated before changing it.

● Read(internal/cache/store.go)
  ⎿  Read 214 lines

● Search(pattern: "Invalidate\\(", path: "internal")
  ⎿  Found 6 files
     internal/cache/store.go
     internal/cache/store_test.go
     internal/server/sessions.go
     internal/server/blocks.go
     internal/server/scripts.go
     internal/server/ui.go

● The cache drops every entry on any write. A write to one session
  shouldn't evict another's, so I'll key invalidation by session.

● Update(internal/cache/store.go)
  ⎿  Updated internal/cache/store.go with 9 additions and 3 removals
       41    func (s *Store) Invalidate(session string) {
       42 -      s.mu.Lock()
       43 -      s.entries = map[string]entry{}
       44 -      s.mu.Unlock()
       42 +      s.mu.Lock()
       43 +      defer s.mu.Unlock()
       44 +      for k, e := range s.entries {
       45 +          if e.session == session {
       46 +              delete(s.entries, k)
       47 +          }
       48 +      }
       49    }

● Bash(go test ./internal/cache/ -run TestInvalidate -count=1)
  ⎿  ok  	github.com/example/app/internal/cache	0.412s

● Invalidation is per session now, and the test passes. Next I'll run the
  whole package and check the server's callers still compile.

● Bash(go build ./... && go test ./internal/... -count=1)
  ⎿  ok  	github.com/example/app/internal/cache	0.398s
     ok  	github.com/example/app/internal/server	3.871s
     ok  	github.com/example/app/internal/script	1.204s

● Done. Summary of the change:
  1. Store.Invalidate takes the session and removes only its entries.
  2. The six callers pass the session they wrote to.
  3. A new test checks another session's entries survive.
"""

NAMES = {b"\x1b[A": "↑", b"\x1b[B": "↓", b"\x1b[C": "→", b"\x1b[D": "←", b"\x1b[H": "Home", b"\x1b[F": "End",
         b"\x1b[5~": "PgUp", b"\x1b[6~": "PgDn", b"\x1b": "Esc", b"\t": "Tab", b"\x7f": "⌫", b"\r": "⏎",
         b"\x1b[Z": "⇧Tab", b"\x1b[1~": "Home", b"\x1b[4~": "End", b"\x1bOH": "Home", b"\x1bOF": "End"}
typed = ""


def draw(*_):
    """As Claude Code does: drawn again at the pane's width whenever it changes, words wrapped whole."""
    cols = max(20, shutil.get_terminal_size((80, 24)).columns)
    lines = []
    for line in WORK.split("\n"):
        indent = len(line) - len(line.lstrip())
        lines += textwrap.wrap(line, cols - 1, subsequent_indent=" " * (indent + 2), drop_whitespace=False) or [""]
    rule = "─" * (cols - 1)
    sys.stdout.write("\x1b[2J\x1b[H")
    sys.stdout.write("\r\n".join(lines))
    sys.stdout.write("\r\n" + rule + "\r\n> " + typed + "\r\n" + rule + "\r\n")
    sys.stdout.write("  ⏵⏵ accept edits on (shift+tab to cycle)")
    sys.stdout.write("\x1b[%d;%dH" % (len(lines) + 3, 3 + len(typed)))
    sys.stdout.flush()


sys.stdout.write("\x1b]0;Cache invalidation per session\x07")
signal.signal(signal.SIGWINCH, draw)
draw()
tty.setraw(0)
while True:
    b = os.read(0, 256)
    if not b:
        break
    if b == b"\x03":
        typed = ""
    elif b in NAMES:
        typed += "[" + NAMES[b] + "]"
    elif b[:1] == b"\x1b":
        typed += "[" + b.decode("utf-8", "replace").replace("\x1b", "⎋") + "]"
    elif b[:1] < b" ":
        typed += "[^" + chr(b[0] + 64) + "]"
    else:
        typed += b.decode("utf-8", "replace")
    draw()
