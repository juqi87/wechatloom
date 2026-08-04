#!/usr/bin/env sh
set -eu

repo="${WECHATLOOM_REPOSITORY:-wechatloom/wechatloom}"
version="${WECHATLOOM_VERSION:-latest}"
install_dir="${WECHATLOOM_INSTALL_DIR:-${HOME}/.local/bin}"

case "$(uname -s)" in
  Darwin) os_name="darwin" ;;
  Linux) os_name="linux" ;;
  *) echo "unsupported operating system" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture" >&2; exit 1 ;;
esac

checksum_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

if [ "$version" = latest ]; then
  version="$(curl --fail --silent --show-error --location "https://api.github.com/repos/${repo}/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -1)"
fi
if [ -z "$version" ]; then
  echo "could not resolve release version" >&2
  exit 1
fi

asset="wechatloom_${version}_${os_name}_${arch}"
base_url="https://github.com/${repo}/releases/download/v${version}"
temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

curl --fail --silent --show-error --location "${base_url}/${asset}" --output "${temporary_dir}/${asset}"
curl --fail --silent --show-error --location "${base_url}/SHA256SUMS" --output "${temporary_dir}/SHA256SUMS"
expected="$(awk -v name="$asset" '$2 == name {print $1}' "${temporary_dir}/SHA256SUMS")"
actual="$(checksum_file "${temporary_dir}/${asset}")"
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "checksum verification failed; nothing was installed" >&2
  exit 1
fi

mkdir -p "$install_dir"
chmod 755 "${temporary_dir}/${asset}"
mv "${temporary_dir}/${asset}" "${install_dir}/wechatloom"
echo "installed wechatloom ${version} at ${install_dir}/wechatloom"
