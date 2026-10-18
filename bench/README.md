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

The app has its own suite, in Swift, built in release:

```sh
cd apps/macos
DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer swift run -c release hivebench-app all   # or feed, frame, pipeline, engine, memory
```

`engine` runs `bin/hive` (`make build`) on its own socket, state and
settings, and needs `/tmp/hivebench` (above) as the pane's program.

## Scenarios

| Scenario | Ramps | Limit |
| --- | --- | --- |
| `flush` | busy panes (100 KB/s each), empty and full scrollback, with snapshot saves running back to back | `agent.report` p99 above 50 ms |
| `ptyread` | busy panes (100 KB/s each), no clients, no saves | under 90% of the offered bytes read |
| `storm` | OSC sequences a second per pane (100 to 100,000), per kind: title, pwd, status, bell, clipboard, notify | API p99 above 50 ms, an events subscriber dropped, under 90% delivered, or the Go heap growing over 64 MB |
| `resize` | resizes a second (10 to 1,000, one caller), alternating sizes, a pane streaming 1 MB/s to an attach client | resize or output p99 above 50 ms, a resync, or the terminal not at the size last asked for |
| `clients` | attach clients (1 to 256), each subscribed to 4 panes streaming 256 KB/s | latency p99 above 50 ms, a resync, or a client short of bytes |
| `soak` | time: steady load (8 busy panes, 2 clients, an events subscriber, agent reports, titles) with churn (a window opened and closed every 10 s, a client reconnecting every 30 s, `--reconnect 0` to turn it off) | Go heap growing over 10 MB an hour, goroutines or open files growing, or an interval's latency p99 above 50 ms |
| `churn` | windows opened and closed, in batches | resident memory per closed window above 16 KB |
| `bench/app-soak.sh` | time: the real app on a throwaway server, 4 busy panes, titles, window churn | none: app RSS growth per hour |
| `bench/app-storm.sh` | the real Hive.app (release) on a throwaway server, a pane storming one OSC kind | none: app CPU and memory growth; `SAMPLE=1` samples its stacks |
| app `feed` | content (plain, SGR colour, unicode, a full-screen TUI) × chunk size | none: µs per KB parsed |
| app `frame` | grid 80x24 to 500x150, every row or one row changed | frame + encode p99 above 8 ms (half a 60 Hz frame) |
| app `pipeline` | grid × chunk, a frame after every chunk | under the server's 64 MB/s |
| app `engine` | output rate, the app's real HiveClient and Engine on a real server | a resync, or the vt queue over 90% of a core |
| app `resize` | a mirror's history (0, 4, 16 MB), resized back and forth, a frame after each | resize + frame p99 above 8 ms |
| app `memory` | mirrors with full scrollback | none: MB per mirror |
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

**2026-10-17, the app** (same machine, on battery, load ~7, release build).
Parsing is cheap: 400–1,500 MB/s (0.7–2.5 µs per KB; a full-screen TUI is
the slowest). Frames are cheap too: no knee up to 500x150 (frame + encode
p99 5.2 ms, GPU 6.8 ms); encode rebuilds every instance even when one row
changed (1.6 ms at 400x120), room left if it ever matters. Memory: ~20 MB
per mirror with full scrollback; the app only mirrors visible panes.

The knee was frame building: the Engine built a frame per output chunk,
up to 5,744 frames/s at 64 MB/s (120x40), and a 300x80 pane saturated the
vt queue from 16 MB/s (1.02 cores). Frames are now paced to 120 Hz
(FramePacer; the first after a quiet spell at once): ~108 frames/s at
every rate, 300x80 at 16 MB/s takes 0.28 cores, and 64 MB/s is no longer
degraded at either size (0.78–0.81 cores, parsing and the socket).

**2026-10-18, storm** (same machine, on battery). The server takes every
kind at 100,000 sequences a second from one pane: all events delivered,
API p99 under 1 ms, saves still once a second, flat heap, 1.1–1.5 cores.

The app had three problems under it, all fixed:
- When the app fell behind on events, the server sent `events_resync` and
  ended the stream; the app re-read once and never subscribed again, so
  titles, agent status and bells stopped until a reconnect. The stream now
  goes on (subscribed again first, then `events_resync`).
- Its main thread spent ~83% of its time asleep in `CAMetalLayer
  nextDrawable`: each title change reloads the view and redraws the panes,
  and a window behind others gets drawables slowly. Panes no longer draw
  while their window is occluded, and draw when it shows. 10 titles a
  second went from 0.19 to 0.12 cores.
- Events waiting for the main thread had no bound: clipboard writes at
  100,000 a second grew the app 447 MB in 6 s. They now go in one bounded
  batch per hop (EventBatch, 4,096); past that, the app re-reads state.
  Same load: +0 MB.

Left: a title change still reloads the whole view (~12 ms of CPU each);
updating it in place would make an agent's title spinner nearly free.

**2026-10-18, resize** (same machine, on battery). No knee on either side.
The server: resize calls 2–3 ms p99 at any rate, output latency p99 under
9 ms, no resyncs, and the terminal always ends at the size last asked for.
One caller gets ~480 resizes a second done, far beyond a window drag
(60–120). At 1 MB/s even the "empty" case fills its scrollback (~26,500
rows), so every resize here reflows a full history, at ~2 ms. The app: a
mirror resize is 1.8 ms p99 with 16 MB of history, plus 0.4 ms for the
frame. Two clients can't fight over a size (only the size owner resizes;
the rest letterbox), so the probe's `take` resizes are the worst case.

**2026-10-18, clients** (same machine, on battery). No knee up to 256
clients on 4 busy panes: every client gets all its bytes, latency p99 at
most 3.8 ms, no resyncs. Cost is linear, ~0.025 cores a client (the bench's
clients run in the same process). Go heap 145 MB and 1,819 goroutines at
256.

A note on titles: updating a pane's title in place from the event,
instead of the coalesced view reload, measured worse in the app (0.22 vs
0.12 cores at 10 titles a second, A/B under the same load), so the reload
stays.

**2026-10-18, soak and churn** (same machine, on battery, alongside work).
- App, 30 min (`app-soak.sh`): RSS 96–110 MB, fitted -14 MB an hour after
  warm-up (flat), CPU 0.07–0.15 cores without a trend. Not covered: drawing
  (the window was behind others).
- Server, 40 min: goroutines (67–69) and open files (22–23) flat, latency
  p99 0.4–0.7 ms, no resyncs. The first run flagged heap growth; most of it
  was the probe (its reader kept every latency sample across intervals,
  fixed). The rerun still drifts: heap by quarter 13.6, 13.8, 17.1,
  18.3 MB; RSS 249 to 293 MB. The heap profile diff (`--heap`) shows only
  in-flight frames (`server.frame`, the client's reads), no retained
  structure; the likely source is the reconnecting client, whose snapshots
  of 8 full panes (~96 MB) every 30 s go through the heap and return to the
  OS lazily. Settled below.
- Server, 2 h × 2, side by side, the same load, with `--reconnect 30s`
  and `--reconnect 0` (on AC, load ~4.5 from other work; this measures
  memory, not latency). **No leak; the drift was the reconnecting client.**
  - Without reconnects: RSS flat at 173–178 MB from minute 1 to 120
    (+3 MB an hour), live heap 9.2–10.4 MB, goroutines 66–70, files 22.
  - With them: a sawtooth, live heap 9.4–52 MB and RSS 178–340 MB, each
    reconnect's snapshots of 8 full panes passing through. The live heap's
    floor doesn't move (9.4 to 9.8 MB, +0.4 MB an hour fitted); RSS's
    floor rises over the first hour (178 to 248 MB) and then holds
    (244–249 MB for the last hour): pages the runtime keeps for the next
    burst, not growth.
  - Latency p99 0.3 ms after the first 20 min in both, no resyncs.
- Churn: 3,000 windows opened (with output) and closed. Memory levels off:
  heap flat at 5.2–5.8 MB from 900 windows, RSS flat at 34 MB from 1,500;
  the growth before that is the event bus's replay ring filling. Native
  memory flat: libghostty frees closed terminals. Goroutines and files flat.

**2026-10-18, Linux** (AWS m7g.xlarge: 4 Neoverse-V1 cores, 16 GB, gp3
root at its default 125 MB/s; Ubuntu 24.04; quiet, load < 0.5 before each
run). The first knee anywhere in the suite, and both causes are the machine:
- flush, empty scrollback: `agent.report` p99 under 5 ms to 128 panes,
  65 ms at 192 (degraded), p50 93 ms at 256. The whole VM was saturated:
  idle ~10%, a run queue of 17–43 on 4 cores, 55% system time. 192 emitter
  processes push ~19 MB/s through ptys; the server itself used 1.4 cores.
  Not a hive lock: the mutex profile's waits are pty output behind snapshot
  encodes, and reports don't take that lock.
- flush, full scrollback (12 MB a pane): saves are disk-bound. Every save
  rewrites each changed pane's whole scrollback: 192 panes are ~2.3 GB,
  p99 25.5 s on a 125 MB/s disk (a call's 30 s deadline cut the first run
  short; saves now get 10 minutes). On the Mac's SSD it doesn't show. With
  ordinary use (20 busy panes, 12 MB each: ~240 MB a save) a slow disk, as
  on a cheap VPS or a Raspberry Pi, would feel it: an incremental snapshot
  (only the rows written since the last save) is the fix to consider when
  remote servers make such machines common.
- Snapshot saves are slower than on the Mac at any size (p99 65 ms at 8
  panes, against ~6 ms): no F_BARRIERFSYNC on Linux, so a full fsync on a
  network disk. Reports don't wait for them.
