# hive benchmarks

The point is to find **bottlenecks** and the **frontier**: the load at which
hive stops keeping up. Every scenario ramps one dimension in steps, measures
each step for a fixed time after a warm-up, and stops after two steps past
its stated limit. The last good step is the **knee**; profiles taken there
name the bottleneck.

```sh
PKG_CONFIG_PATH=$PWD/third_party/ghostty-vt/share/pkgconfig go build -o /tmp/hivebench ./bench/cmd/hivebench
/tmp/hivebench flush            # saves × busy panes × scrollback
/tmp/hivebench flush --panes 32 --profile /tmp/flush32   # cpu + heap profile of one step
```

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
| `flush` | busy panes (100 KB/s each), empty and full scrollback | `agent.report` p99 above 50 ms |

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
