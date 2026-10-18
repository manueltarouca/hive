#!/bin/sh
# A baseline: every server scenario at its defaults, then (on macOS) the app
# suite, on a quiet machine. Refuses to start on battery or with a load
# average above 1.5 (FORCE=1 runs anyway, and the results say they're
# unfit). Copies the results into bench/baselines/<machine>-<date>/, to
# commit and compare reruns against. The soak isn't here: it runs for hours
# on its own.
# Usage: bench/baseline.sh
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
case "$(uname)" in
  Darwin)
    load="$(sysctl -n vm.loadavg | awk '{print $2}')"
    power="$(pmset -g batt | head -1)"
    machine="$(sysctl -n hw.model)";;
  *)
    load="$(cut -d' ' -f1 /proc/loadavg)"
    power=""
    machine="$(cat /sys/devices/virtual/dmi/id/product_name 2>/dev/null || uname -m)";;
esac
if [ "${FORCE:-}" != 1 ]; then
  case "$power" in *Battery*) echo "on battery: plug in (or FORCE=1)"; exit 1;; esac
  if awk "BEGIN{exit !($load > 1.5)}"; then echo "load average $load is above 1.5: wait for a quiet machine (or FORCE=1)"; exit 1; fi
fi
PKG_CONFIG_PATH="$root/third_party/ghostty-vt/share/pkgconfig" go build -o /tmp/hivebench ./bench/cmd/hivebench
make build >/dev/null
stamp="$(mktemp /tmp/hive-baseline.XXXX)"
load1() { case "$(uname)" in Darwin) sysctl -n vm.loadavg | awk '{print $2}';; *) cut -d' ' -f1 /proc/loadavg;; esac; }
for s in flush ptyread output storm resize clients churn; do
  # One scenario's load (flush's full scrollback, above all) mustn't carry
  # into the next: wait for it to settle, up to 10 minutes.
  i=0
  while awk "BEGIN{exit !($(load1) > 1.0)}" && [ $i -lt 60 ]; do sleep 10; i=$((i+1)); done
  echo "== $s (load $(load1))"
  /tmp/hivebench "$s"
done
if [ "$(uname)" = Darwin ]; then
  echo "== app"
  (cd apps/macos && DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer swift run -c release hivebench-app all)
fi
dir="bench/baselines/$(echo "$machine" | tr ' ,' '--')-$(date +%Y%m%d)"
mkdir -p "$dir"
find bench/results -name '*.json' -newer "$stamp" -exec cp {} "$dir/" \;
rm -f "$stamp"
echo "baseline: $dir ($(ls "$dir" | wc -l | tr -d ' ') results)"
