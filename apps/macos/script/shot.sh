#!/bin/sh
# Visual check: run Hive.app against a fixture session and capture it.
# Usage: script/shot.sh [light|dark] [palette|picker|select]  ->  build/shots/<style>.png + crops
set -eu
style="${1:-dark}"
overlay="${2:-}"   # palette | picker
here="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$here/../.." && pwd)"
# Everything lives in the repo's build dir, owned by you: nothing runs from
# a shared, world-writable place.
out="$here/build/shots"; mkdir -p "$out"
tools="$here/build/tools"; mkdir -p "$tools"
for t in winid; do
  [ "$tools/$t" -nt "$here/script/tools/$t.swift" ] || swiftc -O "$here/script/tools/$t.swift" -o "$tools/$t"
done
D="$(mktemp -d "${TMPDIR:-/tmp}/hive-fixture.XXXX")"
export HIVE_SERVER="$D/s.sock" HIVE_STATE_DIR="$D/state"
# Fixture agents report made-up sessions: never resume a real agent.
export HIVE_RESUME_CLAUDE='["sh","-c","echo resumed {id}; exec cat"]'
H="$root/bin/hive"

$H new hive-demo --cwd "$root" >/dev/null
$H -s hive-demo send 'git log --oneline --graph --color=always -- apps/macos/Sources/Hive{Core,Render,Kit}'; $H -s hive-demo send-key enter
# An agent block: reports itself through the hook path and sets a task title.
# Hooks run inside the pane, as an agent runs them: hive ignores reports from
# another agent's process tree (this script may run under one).
agent=$($H -s hive-demo split --split right --ratio 45 --cwd "$root" --keep-open --json -- sh -c '
  printf "{\"session_id\":\"7f3c2a10-demo\",\"cwd\":\"%s\",\"hook_event_name\":\"UserPromptSubmit\"}" "$2" | "$1" agent hook claude
  printf "\033]2;✳ Refactor attach protocol\007"
  printf "\033[38;2;217;119;87m ▐▛███▜▌\033[0m   \033[1mClaude Code\033[0m v2.1.302\r\n"
  printf "\033[38;2;217;119;87m▝▜█████▛▘\033[0m  Opus 5.5 · ~/code/projects/hive\r\n\r\n"
  printf "\033[2m>\033[0m refactor the attach protocol so resizes are sequenced\r\n\r\n"
  printf "\033[38;2;217;119;87m●\033[0m Reading internal/server/attach.go\r\n"
  printf "\033[38;2;217;119;87m●\033[0m Update(internal/block/block.go)\r\n  \033[32m+ f := Frame{Resize: true, Cols: cols}\033[0m\r\n"
  exec cat' sh "$H" "$root" | python3 -I -c 'import json,sys;print(json.load(sys.stdin)["block_ids"][0])')
$H -s hive-demo split -b "$agent" --split below --ratio 40 --label tests --cwd "$root" -- sh -c 'cd internal/session && go test -v -run "Split|Resolve" . 2>&1 | tail -12; exec cat' >/dev/null
$H -s hive-demo run --label logs --focus=false --cwd "$root" -- sh -c 'echo serving on :8080; exec cat' >/dev/null
# A second session with a blocked agent and a finished one.
$H new api-gateway --cwd "$root" -- sh -c '
  printf "{\"session_id\":\"a1\",\"cwd\":\"%s\",\"hook_event_name\":\"Notification\",\"notification_type\":\"permission_prompt\",\"message\":\"Claude needs your permission to use Bash\"}" "$2" | "$1" agent hook claude
  printf "\033]2;✳ Migrate rate limiter to Redis\007"; exec cat' sh "$H" "$root" >/dev/null
$H -s api-gateway split --json --cwd "$root" -- sh -c '
  printf "{\"session_id\":\"a2\",\"cwd\":\"%s\",\"hook_event_name\":\"Stop\"}" "$2" | "$1" agent hook claude
  printf "\033]2;✳ Write changelog\007"; exec cat' sh "$H" "$root" >/dev/null
sleep 2

# EXTRA_TABS=n adds plain tabs, to check the title bar with many tabs.
i=1; while [ "$i" -le "${EXTRA_TABS:-0}" ]; do  # not seq: on macOS, seq 1 0 counts down
  $H -s hive-demo run --label "extra-tab-$i" --focus=false >/dev/null; i=$((i + 1))
done
# The app keeps its preferences in a separate suite for these runs.
suite=xyz.matm.hive.shots
defaults delete "$suite" >/dev/null 2>&1 || true
[ "${SIDEBAR:-0}" = 1 ] && defaults write "$suite" sidebar -bool true
# APPEARANCE=system|day|night stores the theme setting. Use the style
# "system" to test it: then HIVE_STYLE isn't set and the setting decides.
[ -n "${APPEARANCE:-}" ] && defaults write "$suite" appearance -string "$APPEARANCE"
# SESSION=label opens the app in that session instead of the last one.
if [ -n "${SESSION:-}" ]; then
  sid=$($H api call target.resolve "{\"kind\":\"session\",\"target\":\"$SESSION\"}" | python3 -I -c 'import json,sys; print(json.load(sys.stdin)["id"])')
  defaults write "$suite" lastSession -string "$sid"
fi
envstyle="$style"; [ "$style" = system ] && envstyle=""
# Start our own copy of the app and track its PID, so we never touch a Hive
# window the maintainer is using. `open -n` makes it the active app, as a real launch
# would; the PID comes from the log file only our copy has open. Leave
# nothing running behind: the app, the fixture server and its programs.
: > "$out/app.log"
app=
cleanup() {
  [ "${KEEP:-0}" = 1 ] && return
  [ -n "$app" ] && kill "$app" 2>/dev/null || true
  "$H" server stop >/dev/null 2>&1 || true
}
trap cleanup EXIT
open -n --env HIVE_SERVER="$HIVE_SERVER" --env HIVE_STYLE="$envstyle" --env HIVE_OPEN="$overlay" --env HIVE_DEFAULTS_SUITE="$suite" \
  --stdout "$out/app.log" --stderr "$out/app.log" "$here/build/Hive.app"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  app=$(lsof -t "$out/app.log" 2>/dev/null | head -1)
  [ -n "$app" ] && break
  sleep 0.3
done
[ -n "$app" ] || { echo "Hive did not start" >&2; exit 1; }
sleep 4
W=$($tools/winid "$app")
# Only Hive's own windows are captured (by window ID), never a screen
# region, so nothing else on the screen can end up in a shot.
screencapture -x -o -l "$W" "$out/$style.png"
# With the native window shadow and corners, for the website.
screencapture -x -l "$W" "$out/$style-shadow.png"
if [ -n "$overlay" ]; then
  P=$($tools/winid "$app" small)
  screencapture -x -o -l "$P" "$out/$style-$overlay.png"
  cwebp -quiet -q 85 "$out/$style-$overlay.png" -o "$out/$style-$overlay.webp"
fi
cwebp -quiet -q 75 -resize 1400 0 "$out/$style.png" -o "$out/$style.webp"
# 1:1 detail crops (retina pixels): title bar, chip+first tab, a pane header.
ffmpeg -v error -y -i "$out/$style.png" -vf "crop=1400:110:0:0" "$out/$style-titlebar.png"
ffmpeg -v error -y -i "$out/$style.png" -vf "crop=1200:180:1340:80" "$out/$style-header.png"
for f in titlebar header; do cwebp -quiet -q 85 "$out/$style-$f.png" -o "$out/$style-$f.webp"; done
echo "$D" > "$out/fixture"
# Follow-up commands source this: both variables, or a stray server uses
# your real state directory.
{ printf 'export HIVE_SERVER=%s HIVE_STATE_DIR=%s\n' "$HIVE_SERVER" "$HIVE_STATE_DIR"
  printf "export HIVE_RESUME_CLAUDE='%s'\n" "$HIVE_RESUME_CLAUDE"; } > "$out/fixture.env"
echo "$out/$style.webp"
