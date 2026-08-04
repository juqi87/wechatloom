#!/usr/bin/env sh
set -eu

version="${1:?version is required}"
base_url="${2:?release base URL is required}"
output="${3:-dist/update-manifest.json}"

assets=""
artifacts_file="dist/artifacts.json"
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required to read GoReleaser artifacts" >&2
  exit 1
fi
if [ ! -f "$artifacts_file" ]; then
  echo "missing GoReleaser artifact index: $artifacts_file" >&2
  exit 1
fi
for os_name in darwin linux windows; do
  for arch in amd64 arm64; do
    if [ "$os_name" = windows ] && [ "$arch" = arm64 ]; then
      continue
    fi
    filename="wechatloom_${version}_${os_name}_${arch}"
    if [ "$os_name" = windows ]; then
      filename="${filename}.exe"
    fi
    path="$(jq -r --arg os "$os_name" --arg arch "$arch" --arg name "$filename" \
      '[.[] | select(.type == "Binary" and .goos == $os and .goarch == $arch and .name == $name)][0].path // empty' \
      "$artifacts_file")"
    if [ -z "$path" ] || [ ! -f "$path" ]; then
      echo "missing release artifact for ${os_name}/${arch}: ${filename}" >&2
      exit 1
    fi
    checksum="$(sha256sum "$path" | awk '{print $1}')"
    entry="{\"os\":\"${os_name}\",\"arch\":\"${arch}\",\"url\":\"${base_url}/${filename}\",\"sha256\":\"${checksum}\"}"
    if [ -n "$assets" ]; then
      assets="${assets},${entry}"
    else
      assets="$entry"
    fi
  done
done

mkdir -p "$(dirname "$output")"
printf '{"schema_version":"1","version":"%s","page_url":"%s","assets":[%s]}\n' \
  "$version" "$base_url" "$assets" > "$output"
