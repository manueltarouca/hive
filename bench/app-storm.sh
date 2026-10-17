#!/bin/sh
# The app under an event storm: the real Hive.app (release) on a throwaway
# server (its own socket, state and settings), showing a pane that writes
# OSC sequences of KIND at each rate. Measures the app's CPU and memory
# growth from ps: the app hops every event onto the main thread, so one it
# can't keep up with queues there and memory climbs.
# Usage: bench/app-storm.sh [kind] [rates…]   (default: title 1000 10000 100000)
# Needs /tmp/hivebench (bench/README.md). Opens a background window per step.
# SAMPLE=1 also samples the app's stacks (/tmp/hive-app-storm-KIND-RATE.sample.txt).
set -eu
kind="${1:-title}"; shift || true
rates="${*:-1000 10000 100000}"
root="$(cd "$(dirname "$0")/.." && pwd)"
out="/tmp/hive-bench-app"
mkdir -p "$out"
HIVE_BUNDLE_OUT="$out" "$root/apps/macos/script/bundle.sh" release >/dev/null
app="$out/Hive.app"
H="$root/bin/hive"
for rate in $rates; do
  d="$(mktemp -d /tmp/hive-appstorm.XXXX)"
  export HIVE_SERVER="$d/s.sock" HIVE_STATE_DIR="$d/state" HIVE_CONFIG="$d/config" HIVE_SESSION= HIVE_BLOCK=
  "$H" server start >/dev/null 2>&1
  "$H" new storm --json -- /tmp/hivebench emit --rate 0 --osc "$kind" --osc-rate "$rate" >/dev/null
  sid="$("$H" api call target.resolve '{"kind":"session","target":"storm"}' | python3 -I -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
  suite=xyz.matm.hive.bench
  defaults delete $suite >/dev/null 2>&1 || true
  defaults write $suite lastSession -string "$sid"
  : > "$d/app.log"
  open -n --env HIVE_SERVER="$HIVE_SERVER" --env HIVE_CONFIG="$HIVE_CONFIG" --env HIVE_DEFAULTS_SUITE=$suite \
    --stdout "$d/app.log" --stderr "$d/app.log" "$app"
  pid=""
  for i in $(seq 30); do pid="$(lsof -t "$d/app.log" 2>/dev/null | head -1)"; [ -n "$pid" ] && break; sleep 0.3; done
  sleep 3
  sample() { ps -o rss=,time= -p "$pid" | awk '{split($2,t,":"); s=0; for(i=1;i<=length(t);i++) s=s*60+t[i]; print $1, s}'; }
  set -- $(sample); r0=$1; c0=$2
  sleep 6
  set -- $(sample); r1=$1; c1=$2
  [ -n "${SAMPLE:-}" ] && /usr/bin/sample "$pid" 3 -file "/tmp/hive-app-storm-$kind-$rate.sample.txt" >/dev/null 2>&1
  resyncs="$(grep -c 'resync' "$d/app.log" 2>/dev/null || true)"
  srv="$(grep -ci 'resync\|dropped\|behind' "$HIVE_STATE_DIR/server.log" 2>/dev/null || true)"
  echo "$kind $rate/s | app log resync lines ${resyncs:-0}, server log ${srv:-0} | app cpu $(echo "($c1 - $c0) / 6" | bc -l | cut -c1-4) cores | rss $((r1/1024)) MB, +$(((r1 - r0)/1024)) MB in 6 s"
  kill "$pid" 2>/dev/null || true
  "$H" server stop >/dev/null 2>&1 || true
  defaults delete $suite >/dev/null 2>&1 || true
  [ -n "${KEEP:-}" ] && { echo "kept $d"; continue; }
  case "$d" in /tmp/hive-appstorm.*) rm -rf "$d";; esac
done
