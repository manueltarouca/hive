#!/bin/sh
# Packages a release for this machine into dist/:
#   hive-VERSION-OS-ARCH.tar.gz      the hive binary, LICENSE, README, notices
#                                    (OS is macos or linux, ARCH arm64 or amd64)
#   Hive-VERSION-macos-arm64.zip     Hive.app (macOS only)
#   SHA256SUMS
# VERSION comes from the git tag through the Makefile; set VERSION= to override.
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

version="$(make -s version)"
case "$(uname -s)" in
  Darwin) os=macos ;;
  Linux) os=linux ;;
  *) echo "unsupported system: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

make build >/dev/null
mkdir -p dist
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The notices list what libghostty-vt bundles at the pinned revision. If the
# archive gains an object they don't cover, stop rather than ship without it.
known="abort.o base64.o codepoint_width.o compiler_rt.o index_of.o libghostty-vt-static_zcu.o libhighway_zcu.o per_target.o simdutf.o targets.o vt.o wuffs-v0.4.o"
# GNU ar prints the path an object was built at; compare base names.
for o in $(ar t third_party/ghostty-vt/lib/libghostty-vt.a | grep '\.o$' | sed 's#.*/##' | sort -u); do
  case " $known " in *" $o "*) ;; *) echo "libghostty-vt has $o, which script/notices doesn't cover" >&2; exit 1 ;; esac
done

# Licences of everything linked into hive: Ghostty's terminal library and the
# Go modules the binary actually uses.
notices="$work/THIRD_PARTY_NOTICES"
{
  echo "hive includes the following third-party software."
  echo
  echo "== Ghostty (libghostty-vt) =="
  echo
  cat third_party/ghostty/LICENSE
  echo
  cat script/notices/ghostty-vt-deps.txt
  go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Dir}}{{end}}{{end}}' ./cmd/hive | sort -u |
    while read -r path dir; do
      file="$(ls "$dir" | grep -iE '^(license|copying)' | head -1)"
      [ -n "$file" ] || { echo "no licence file for $path" >&2; exit 1; }
      printf '\n== %s ==\n\n' "$path"
      cat "$dir/$file"
    done
} > "$notices"

name="hive-$version-$os-$arch"
mkdir "$work/$name"
cp bin/hive LICENSE README.md "$notices" "$work/$name/"
tar -C "$work" -czf "dist/$name.tar.gz" "$name"
echo "dist/$name.tar.gz"

if [ "$os" = macos ]; then
  app="$(apps/macos/script/bundle.sh release | tail -1)"
  { cat "$notices"; echo; cat script/notices/fonts.txt; } > "$app/Contents/Resources/THIRD_PARTY_NOTICES"
  # Re-sign after adding the file, or the signature no longer matches. Clear
  # extended attributes first: they would ship as ._ files that break the
  # seal when the zip is opened with unzip.
  xattr -cr "$app"
  codesign --force --deep --sign - "$app" >/dev/null
  zip="dist/Hive-$version-macos-$arch.zip"
  rm -f "$zip"
  ditto -c -k --keepParent --norsrc "$app" "$zip"
  echo "$zip"
fi

sum() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
(cd dist && sum hive-* $(ls Hive-* 2>/dev/null) > SHA256SUMS)
echo dist/SHA256SUMS
