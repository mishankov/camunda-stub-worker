#!/bin/bash

set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
  echo "Usage: $0 <app-path> <output.dmg> [volume-name]" >&2
  exit 2
fi

app_path=$1
output_path=$2
volume_name=${3:-Camunda Stub Worker}
app_name=$(basename "$app_path")

if [[ $(uname -s) != Darwin ]]; then
  echo "DMG creation requires macOS." >&2
  exit 1
fi

if [[ ! -d "$app_path" ]]; then
  echo "Application bundle not found: $app_path" >&2
  exit 1
fi

volume_icon="$app_path/Contents/Resources/iconfile.icns"
if [[ ! -f "$volume_icon" ]]; then
  echo "Application icon not found: $volume_icon" >&2
  exit 1
fi

output_dir=$(dirname "$output_path")
mkdir -p "$output_dir"
output_path=$(cd "$output_dir" && pwd)/$(basename "$output_path")

work_dir=$(mktemp -d)
staging_dir="$work_dir/staging"
read_write_dmg="$work_dir/read-write.dmg"
device=""

detach_image() {
  local attempt

  for attempt in 1 2 3 4 5; do
    if hdiutil detach "$device"; then
      device=""
      return 0
    fi

    if [[ $attempt -lt 5 ]]; then
      echo "DMG is still busy; retrying detach ($attempt/5)..." >&2
      sleep $((attempt * 2))
    fi
  done

  echo "Failed to detach $device after 5 attempts." >&2
  return 1
}

cleanup() {
  if [[ -n "$device" ]]; then
    hdiutil detach -force "$device" || true
  fi
  rm -rf "$work_dir"
}
trap cleanup EXIT

mkdir -p "$staging_dir"
ditto "$app_path" "$staging_dir/$app_name"
ln -s /Applications "$staging_dir/Applications"
hdiutil create -quiet \
  -volname "$volume_name" \
  -srcfolder "$staging_dir" \
  -fs HFS+ \
  -format UDRW \
  "$read_write_dmg"

attach_output=$(hdiutil attach -readwrite -noverify -noautoopen "$read_write_dmg")
device=$(printf '%s\n' "$attach_output" | awk '/^\/dev\// {print $1; exit}')
mount_point=$(printf '%s\n' "$attach_output" | tail -n 1 | awk -F '\t' '{print $NF}')

echo "Configuring Finder layout..."
osascript - "$volume_name" "$app_name" <<'APPLESCRIPT'
on run argv
  set volumeName to item 1 of argv
  set applicationName to item 2 of argv

  tell application "Finder"
    tell disk volumeName
      open
      set current view of container window to icon view
      set toolbar visible of container window to false
      set statusbar visible of container window to false
      set pathbar visible of container window to false
      set bounds of container window to {400, 150, 980, 510}

      set viewOptions to icon view options of container window
      set arrangement of viewOptions to not arranged
      set icon size of viewOptions to 112
      set text size of viewOptions to 14

      set position of item applicationName of container window to {145, 175}
      set position of item "Applications" of container window to {435, 175}

      update without registering applications
      delay 2
      close
    end tell
  end tell
end run
APPLESCRIPT

ditto "$volume_icon" "$mount_point/.VolumeIcon.icns"
SetFile -c icnC "$mount_point/.VolumeIcon.icns"
SetFile -a C "$mount_point"

if [[ $(GetFileInfo -a "$mount_point") != *C* ]]; then
  echo "Failed to set the DMG volume's custom icon attribute." >&2
  exit 1
fi

echo "Configuring the volume to open in Finder..."
if [[ $(uname -m) == arm64 ]]; then
  bless --folder "$mount_point"
else
  bless --folder "$mount_point" --openfolder "$mount_point"
fi

sync
echo "Detaching writable image..."
detach_image

echo "Compressing final image..."
hdiutil convert -quiet \
  "$read_write_dmg" \
  -format UDZO \
  -imagekey zlib-level=9 \
  -ov \
  -o "$output_path"

echo "Created $output_path"
