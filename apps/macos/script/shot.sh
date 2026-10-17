#!/bin/sh
# Visual check: run Hive.app against a fixture session and capture it.
# Usage: script/shot.sh [light|dark]  ->  /tmp/hive-shot/<style>.png + crops
set -eu
style="${1:-dark}"
overlay="${2:-}"   # palette | picker
here="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$here/../.." && pwd)"
out=/tmp/hive-shot; mkdir -p "$out"
tools=/tmp/hive-tools
D="$(mktemp -d /tmp/hive-fixture.XXXX)"
export HIVE_SERVER="$D/s.sock" HIVE_STATE_DIR="$D/state"
H="$root/bin/hive"

$H new hive-demo --cwd "$root" >/dev/null
$H -s hive-demo send 'git log --oneline --graph -14 --color=always'; $H -s hive-demo send-key enter
# An agent block: reports itself through the hook path and sets a task title.
agent=$($H -s hive-demo split --split right --ratio 45 --cwd "$root" --keep-open --json -- sh -c '
  printf "\033]2;✳ Refactor attach protocol\007"
  printf "\033[38;2;217;119;87m ▐▛███▜▌\033[0m   \033[1mClaude Code\033[0m v2.1.302\r\n"
  printf "\033[38;2;217;119;87m▝▜█████▛▘\033[0m  Opus 5.5 · ~/code/projects/hive\r\n\r\n"
  printf "\033[2m>\033[0m refactor the attach protocol so resizes are sequenced\r\n\r\n"
  printf "\033[38;2;217;119;87m●\033[0m Reading internal/server/attach.go\r\n"
  printf "\033[38;2;217;119;87m●\033[0m Update(internal/block/block.go)\r\n  \033[32m+ f := Frame{Resize: true, Cols: cols}\033[0m\r\n"
  exec cat' | python3 -I -c 'import json,sys;print(json.load(sys.stdin)["block_ids"][0])')
printf '{"session_id":"7f3c2a10-demo","cwd":"%s","hook_event_name":"UserPromptSubmit"}' "$root" | HIVE_BLOCK=$agent $H agent hook claude
$H -s hive-demo split -b "$agent" --split below --ratio 40 --label tests --cwd "$root" -- sh -c 'cd internal/session && go test -v -run "Split|Resolve" . 2>&1 | tail -12; exec cat' >/dev/null
$H -s hive-demo run --label logs --focus=false --cwd "$root" -- sh -c 'echo serving on :8080; exec cat' >/dev/null
sleep 2

pkill -f 'Hive.app/Contents/MacOS/Hive' 2>/dev/null || true
defaults delete xyz.matm.hive >/dev/null 2>&1 || true
open -n --env HIVE_SERVER="$HIVE_SERVER" --env HIVE_STYLE="$style" --env HIVE_OPEN="$overlay" "$here/build/Hive.app"
sleep 4
W=$($tools/winid Hive)
# Only Hive's own windows are captured (by window ID), never a screen
# region, so nothing else on the screen can end up in a shot.
screencapture -x -o -l "$W" "$out/$style.png"
if [ -n "$overlay" ]; then
  P=$($tools/winid Hive small)
  screencapture -x -o -l "$P" "$out/$style-$overlay.png"
  cwebp -quiet -q 85 "$out/$style-$overlay.png" -o "$out/$style-$overlay.webp"
fi
cwebp -quiet -q 75 -resize 1400 0 "$out/$style.png" -o "$out/$style.webp"
# 1:1 detail crops (retina pixels): title bar, chip+first tab, a pane header.
ffmpeg -v error -y -i "$out/$style.png" -vf "crop=1400:110:0:0" "$out/$style-titlebar.png"
ffmpeg -v error -y -i "$out/$style.png" -vf "crop=1200:180:1340:80" "$out/$style-header.png"
for f in titlebar header; do cwebp -quiet -q 85 "$out/$style-$f.png" -o "$out/$style-$f.webp"; done
echo "$D" > "$out/fixture"
echo "$out/$style.webp"
