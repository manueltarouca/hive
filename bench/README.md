# hive benchmarks

The point is to find **bottlenecks** and the **frontier**: the load at which
hive stops keeping up. Every scenario ramps one dimension in steps, measures
each step for a fixed time after a warm-up, and stops after two steps past
its stated limit. The last good step is the **knee**; profiles taken there
name the bottleneck.

```sh
PKG_CONFIG_PATH=$PWD/third_party/ghostty-vt/share/pkgconfig go build -o /tmp/hivebench ./bench/cmd/hivebench
/tmp/hivebench flush            # saves × busy panes × scrollback
/tmp/hivebench flush --panes 32 --profile /tmp/flush32   # cpu, heap, mutex and block profiles of one step
/tmp/hivebench output           # output rate → latency and throughput to an attach client
/tmp/hivebench ptyread --max 32  # busy panes → pty read cost alone (process CPU via getrusage)
```

On macOS, CPU profiles over-count threads blocked in syscalls: a pane's
reader waiting in `read` shows up as CPU it doesn't use. Trust `cpu_cores`
(getrusage) for how much, and the profile only for where.

Each run writes `bench/results/<time>-<scenario>.json` with the machine,
versions, commit, power source, load average, pty limits and every step. A
run on battery or with a load average above 1.5 is marked unfit for a
baseline. The server runs in-process in a temp dir: never your server,
state or settings.

Memory is reported as the process's resident size, Go's heap, and the rest
(native): terminal state lives in libghostty's memory, which Go's heap
profile doesn't see.

## Scenarios

| Scenario | Ramps | Limit |
| --- | --- | --- |
| `flush` | busy panes (100 KB/s each), empty and full scrollback, with snapshot saves running back to back | `agent.report` p99 above 50 ms |
| `ptyread` | busy panes (100 KB/s each), no clients, no saves | under 90% of the offered bytes read |
| `output` | output rate per pane (64 KB/s to 128 MB/s), stamped by the emitter, read by an attach client as the app does; `--client-cost` makes the client spend time on each frame, as the app does parsing | latency p99 above 50 ms, under 90% of the bytes delivered, or a resync |

## Findings

**2026-10-17, flush** (M5 Pro, on battery, load ~6; for knees, not a baseline).
`agent.report` saved every changed terminal synchronously, and each snapshot
cost ~5 ms (F_FULLFSYNC ~4 ms + unbuffered writes ~1.2 ms): p50 40 ms at 8
busy panes, 152 ms at 32. Claude Code waits for its hooks, so with many busy
agents every hook stalled them. Fixed: reports save only state.json, and
snapshots are buffered and not forced to disk. After: p50 ~5 ms flat to 256
panes; with full scrollback the knee moved from 1 to 64 panes. Next knee:
at 128+ panes with full scrollback, a timed save of all snapshots holds the
save lock for up to 1.5 s, and a report waits behind it.

**2026-10-17, flush, again** (same machine, on battery, load ~7). The first
probe never ran a snapshot save while measuring: output doesn't mark state
dirty, so only the 30 s tick snapshots, and 8 s steps mostly missed it. The
probe now runs `server.flush` back to back during each step. At 128 busy
panes, `agent.report` p99, empty / full scrollback:

| | empty | full |
| --- | --- | --- |
| one save lock (before) | 218 ms | 1440 ms |
| snapshots and state.json under separate locks | 21 ms | 220 ms |
| state.json synced with F_BARRIERFSYNC, not F_FULLFSYNC | 6.7 ms | 272 ms |
| the report saves in the background | 1.8 ms | 186 ms |
| a terminal's size read without its lock | 3.3 ms | 107 ms |

Each fix was found by measuring the one before it: the report's own save
split into save-lock wait (~5 ms) and the rest (203 ms: mostly a full
cache flush behind 1.5 GB of snapshots; it also counts `s.mu`, encoding
and writing), then state saves stuck behind `s.mu`, which
`saveState` held while `Term.Size` waited ~150 ms for a snapshot's encode.
Full scrollback at 128 panes stays degraded (p99 85–107 ms, p50 under
1 ms). The CPU profile there said 86% pty `read` syscalls, but measured
with getrusage (`ptyread`), reading costs 0.10 cores at 32 busy panes
(3.3 MB/s, ~536 bytes and 16 µs a read): the profile was the macOS
sampling bias, not the bottleneck. What does cost CPU is encoding
snapshots: `flush` at 32 full panes uses 1.44 cores with saves back to
back, against 0.40 with empty scrollback. At 128 that's several cores on a
machine already at load ~8, which likely explains the tail; the probe's
back-to-back saves are far harsher than the real 30 s tick. To confirm on
a quiet machine. Memory: ~12 MB native per busy pane
(16 MB scrollback filling), 1.6 GB at 128 panes.

**2026-10-17, output** (same machine, on battery, load ~7). One pane, one
client: everything arrives up to 64 MB/s, latency p99 0.2–1 ms; at
128 MB/s 109–113 MB/s arrives (knee 64 MB/s). For scale, the emitter
writes 256 MB/s and a bare pty read by Python passes ~140 MB/s. A macOS
pty gives ~1 KB a read (133,000 reads/s at 128 MB/s; reading alone costs
0.53 cores, `ptyread`), and each read was its own WebSocket frame.

With a client that spends time on each frame (`--client-cost`), the
server fell behind it and resynced it over and over: at 64 MB/s, 289
resyncs in 6 s with 20 µs a frame (53% delivered), 200 with 50 µs (25%),
and 350 MB of RSS. Output already queued for a subscriber now goes out as
one frame (up to 256 KB): 0 resyncs, 101% delivered, 41 MB, frames of
1.7–3.9 KB as the client needs. A fast client is unchanged (nothing is
queued, nothing waits): its cost is the hand-offs per 1 KB read.
