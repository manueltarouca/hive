#!/bin/sh
# A pane that tracks the mouse, clicked through the window's own dispatch in
# a throwaway debug Hive.app (build/remote-check). The program must see the
# press and the release: ^[[<0;C;RM then ^[[<0;C;Rm. CX: points into the
# pane (40 sits under the closed sidebar). Never the user's app or server.
set -eu
here="$(cd "$(dirname "$0")/.." && pwd)"; root="$(cd "$here/../.." && pwd)"; out="$here/build/ux"; H="$root/bin/hive"
mkdir -p "$out"
D="$(mktemp -d /tmp/hive-ux.XXXX)"
export HIVE_SERVER="$D/s.sock" HIVE_STATE_DIR="$D/state" HIVE_CONFIG="$D/config" HIVE_SESSION= HIVE_BLOCK=
$H new clicktest --cwd /tmp -- sh -c "stty raw -echo; printf '\033[?1000h\033[?1006htracking on\r\n'; exec cat -v" >/dev/null
suite=xyz.matm.hive.ux; defaults delete "$suite" >/dev/null 2>&1 || true
: > "$out/app.log"
open -n --env HIVE_SERVER="$HIVE_SERVER" --env HIVE_CONFIG="$HIVE_CONFIG" --env HIVE_STATE_DIR="$HIVE_STATE_DIR" \
  --env HIVE_DRAW_COVERED=1 --env HIVE_OPEN=click --env HIVE_CLICK_X="${CX:-40}" --env HIVE_DEFAULTS_SUITE="$suite" \
  --stdout "$out/app.log" --stderr "$out/app.log" "$here/build/remote-check/Hive.app"
app=; for _ in 1 2 3 4 5 6 7 8 9 10; do app=$(lsof -t "$out/app.log" 2>/dev/null | head -1); [ -n "$app" ] && break; sleep 0.3; done
sleep 5
W=$("$here/build/tools/winid" "$app" small); screencapture -x -o -l "$W" "$out/click.png"
kill "$app" 2>/dev/null || true; $H server stop >/dev/null 2>&1 || true; defaults delete "$suite" >/dev/null 2>&1 || true
echo "$out/click.png"
