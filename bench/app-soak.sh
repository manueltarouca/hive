#!/bin/sh
# The app over time: the real Hive.app (release) on a throwaway server (its
# own socket, state and settings), showing a session with busy panes, a
# title a second, and a window opened and closed every 10 s. Samples the
# app's RSS and CPU every 30 s and fits growth per hour after a warm-up.
# The window opens behind others, so panes don't draw: this covers the
# client, mirrors, events and SwiftUI, not rendering.
# Usage: bench/app-soak.sh [minutes]   (default 30). Needs /tmp/hivebench.
set -eu
minutes="${1:-30}"
root="$(cd "$(dirname "$0")/.." && pwd)"
out="/tmp/hive-bench-app"
mkdir -p "$out"
HIVE_BUNDLE_OUT="$out" "$root/apps/macos/script/bundle.sh" release >/dev/null 2>&1
app="$out/Hive.app"
H="$root/bin/hive"
d="$(mktemp -d /tmp/hive-appsoak.XXXX)"
export HIVE_SERVER="$d/s.sock" HIVE_STATE_DIR="$d/state" HIVE_CONFIG="$d/config" HIVE_SESSION= HIVE_BLOCK=
cleanup() {
  [ -n "${pid:-}" ] && kill "$pid" 2>/dev/null || true
  "$H" server stop >/dev/null 2>&1 || true
  defaults delete xyz.matm.hive.bench >/dev/null 2>&1 || true
  case "$d" in /tmp/hive-appsoak.*) rm -rf "$d";; esac
}
trap cleanup EXIT
"$H" server start >/dev/null 2>&1
emit="/tmp/hivebench emit --rate 102400 --osc title --osc-rate 1"
"$H" new soak --json -- $emit >/dev/null
for i in 2 3 4; do "$H" -s soak run --label "busy$i" --focus=false -- $emit >/dev/null; done
sid="$("$H" api call target.resolve '{"kind":"session","target":"soak"}' | python3 -I -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
suite=xyz.matm.hive.bench
defaults delete $suite >/dev/null 2>&1 || true
defaults write $suite lastSession -string "$sid"
open -n --env HIVE_SERVER="$HIVE_SERVER" --env HIVE_CONFIG="$HIVE_CONFIG" --env HIVE_DEFAULTS_SUITE=$suite \
  --stdout "$d/app.log" --stderr "$d/app.log" "$app"
pid=""
for i in $(seq 30); do pid="$(lsof -t "$d/app.log" 2>/dev/null | head -1)"; [ -n "$pid" ] && break; sleep 0.3; done
# Window churn, in the background.
( while kill -0 "$pid" 2>/dev/null; do
    "$H" -s soak run --label churn --focus=false -- sh -c 'echo churn; sleep 1' >/dev/null 2>&1 || true
    sleep 10
  done ) &
samples="$d/samples.txt"
: > "$samples"
start=$(date +%s)
end=$((start + minutes * 60))
prev_cpu=""
while [ "$(date +%s)" -lt "$end" ]; do
  sleep 30
  set -- $(ps -o rss=,time= -p "$pid" | awk '{split($2,t,":"); s=0; for(i=1;i<=length(t);i++) s=s*60+t[i]; print $1, s}')
  now=$(( $(date +%s) - start ))
  cores="-"
  [ -n "$prev_cpu" ] && cores="$(echo "($2 - $prev_cpu) / 30" | bc -l | cut -c1-4)"
  prev_cpu=$2
  printf '%6.1f min | app rss %5d MB | cpu %s cores\n' "$(echo "$now / 60" | bc -l)" $(( $1 / 1024 )) "$cores"
  echo "$now $1" >> "$samples"
done
# Growth per hour of RSS after 5 minutes (least squares).
awk '$1 >= 300 {n++; x=$1/60; y=$2/1024; sx+=x; sy+=y; sxy+=x*y; sxx+=x*x}
  END { if (n > 1) printf "app rss growth after 5 min: %+.1f MB per hour (%d samples)\n", (n*sxy - sx*sy) / (n*sxx - sx*sx) * 60, n }' "$samples"
