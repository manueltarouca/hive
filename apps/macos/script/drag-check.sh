#!/bin/sh
# Dragging a tab to a new place, in a throwaway debug Hive.app (build/remote-check) on a
# throwaway server: the window before, mid-drag (the others make room: the preview), and
# after it lands, plus the server's tab order. Never the user's app or server.
# Usage: script/drag-check.sh bar|sidebar INDEX:BY [night|day]
#   INDEX: the shown tab to drag (0 is the first); BY: how many tabs along (negative: back).
#   The drag's own steps are driven in the app (HIVE_OPEN=reorder): SwiftUI takes the mouse only in
#   the key window, and this never takes focus. A real pointer drag needs the window in front.
set -eu
layout="${1:-bar}"; drag="${2:-0:2.6}"; style="${3:-night}"
here="$(cd "$(dirname "$0")/.." && pwd)"; root="$(cd "$here/../.." && pwd)"; out="$here/build/drag-$layout-$style"; H="$root/bin/hive"
mkdir -p "$out" "$here/build/tools"
[ "$here/build/tools/winid" -nt "$here/script/tools/winid.swift" ] || swiftc -O "$here/script/tools/winid.swift" -o "$here/build/tools/winid"
D="$(mktemp -d /tmp/hive-drag.XXXX)"
export HIVE_SERVER="$D/s.sock" HIVE_STATE_DIR="$D/state" HIVE_CONFIG="$D/config" HIVE_SCRIPTS="$D/scripts" HIVE_SESSION= HIVE_BLOCK=
$H new tabs --cwd /tmp >/dev/null
sid=$($H api call session.list | python3 -c "import json,sys; print(json.load(sys.stdin)['sessions'][0]['session_id'])")
w0=$($H api call session.view "{\"session_id\":\"$sid\"}" | python3 -c "import json,sys; print(json.load(sys.stdin)['windows'][0]['window_id'])")
$H api call session.set_window_label "{\"session_id\":\"$sid\",\"window_id\":\"$w0\",\"label\":\"one\"}" >/dev/null
for l in two three four five; do
  w=$($H api call session.new_window "{\"session_id\":\"$sid\",\"layout\":{\"cwd\":\"/tmp\"}}" | python3 -c "import json,sys; print(json.load(sys.stdin)['window_id'])")
  $H api call session.set_window_label "{\"session_id\":\"$sid\",\"window_id\":\"$w\",\"label\":\"$l\"}" >/dev/null
done
order() { $H api call session.view "{\"session_id\":\"$sid\"}" | python3 -c "import json,sys; print(' '.join(w['label'] for w in json.load(sys.stdin)['windows']))"; }
echo "before: $(order)"
suite=xyz.matm.hive.drag; defaults delete "$suite" >/dev/null 2>&1 || true
defaults write "$suite" lastSession "$sid"
[ "$layout" = sidebar ] && defaults write "$suite" sidebar -bool true || defaults write "$suite" sidebar -bool false
defaults write "$suite" appearance "$style"
: > "$out/app.log"
open -g -n --env HIVE_SERVER="$HIVE_SERVER" --env HIVE_CONFIG="$HIVE_CONFIG" --env HIVE_STATE_DIR="$HIVE_STATE_DIR" \
  --env HIVE_DRAW_COVERED=1 --env HIVE_OPEN="reorder:$layout-tabs:$drag" --env HIVE_DEFAULTS_SUITE="$suite" \
  --stdout "$out/app.log" --stderr "$out/app.log" "$here/build/remote-check/Hive.app"
app=; for _ in 1 2 3 4 5 6 7 8 9 10; do app=$(lsof -t "$out/app.log" 2>/dev/null | head -1); [ -n "$app" ] && break; sleep 0.3; done
sleep 2.2
W=$("$here/build/tools/winid" "$app" small); screencapture -x -o -l "$W" "$out/before.png"
sleep 2.4   # the drag starts at 2.5 s and moves for about 1 s
screencapture -x -o -l "$W" "$out/during.png"
sleep 4.5
screencapture -x -o -l "$W" "$out/after.png"
echo "after:  $(order)"; echo "active: $($H api call session.view "{\"session_id\":\"$sid\"}" | python3 -c "import json,sys; v=json.load(sys.stdin); print(next(w['label'] for w in v['windows'] if w['window_id']==v['active_window_id']))")"
kill "$app" 2>/dev/null || true; $H server stop >/dev/null 2>&1 || true; defaults delete "$suite" >/dev/null 2>&1 || true; rm -rf "$D"
echo "$out"
