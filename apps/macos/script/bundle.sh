#!/bin/sh
# Builds Hive.app: the Swift app, the hive server binary, fonts.
# Usage: script/bundle.sh [debug|release]   (default release)
set -eu
config="${1:-release}"
here="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$here/../.." && pwd)"
export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"

make -C "$root" build >/dev/null
cd "$here"
swift build -c "$config" --product Hive
bin="$(swift build -c "$config" --show-bin-path)"

app="$here/build/Hive.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin/Hive" "$app/Contents/MacOS/Hive"
cp "$root/bin/hive" "$app/Contents/Resources/hive"
cp -R "$bin/Hive_HiveRender.bundle" "$app/Contents/Resources/"

# The icon, redrawn only when its source changes.
icns="$here/build/AppIcon.icns"
if [ ! -f "$icns" ] || [ "$here/script/icon.swift" -nt "$icns" ]; then
  set_dir="$(mktemp -d)/AppIcon.iconset"
  mkdir -p "$set_dir"
  swift "$here/script/icon.swift" "$set_dir/icon_512x512@2x.png"
  for s in 16 32 128 256 512; do
    sips -z $s $s "$set_dir/icon_512x512@2x.png" --out "$set_dir/icon_${s}x${s}.png" >/dev/null
    d=$((s * 2))
    [ $s = 512 ] || sips -z $d $d "$set_dir/icon_512x512@2x.png" --out "$set_dir/icon_${s}x${s}@2x.png" >/dev/null
  done
  iconutil -c icns "$set_dir" -o "$icns"
fi
cp "$icns" "$app/Contents/Resources/AppIcon.icns"
cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleIdentifier</key><string>xyz.matm.hive</string>
  <key>CFBundleName</key><string>Hive</string>
  <key>CFBundleDisplayName</key><string>Hive</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleExecutable</key><string>Hive</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>14.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSPrincipalClass</key><string>NSApplication</string>
</dict></plist>
PLIST
codesign --force --deep --sign - "$app" >/dev/null
echo "$app"
