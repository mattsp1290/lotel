#!/bin/sh
set -eu

default_base="https://github.com/mattsp1290/lotel/releases"
tmp=
version_output=
version_pid=
watchdog_pid=

fail() {
  printf 'install.sh: %s\n' "$1" >&2
  exit 1
}

cleanup() {
  [ -z "$watchdog_pid" ] || kill "$watchdog_pid" 2>/dev/null || true
  [ -z "$version_pid" ] || kill -KILL "$version_pid" 2>/dev/null || true
  [ -z "$version_output" ] || rm -f "$version_output"
  if [ -n "$tmp" ]; then
    rm -f "$tmp"
  fi
}

valid_tag() {
  case $1 in
    '' | *[!0-9.v]*) return 1 ;;
  esac
  printf '%s\n' "$1" |
    awk '/^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/ { ok = 1 } END { exit !ok }'
}

fetch() {
  max=$1 url=$2 out=$3
  set -- -q -fsSL --globoff --connect-timeout 10 --retry 3 --retry-max-time "$max" --max-time "$max"
  if [ "$secure" = 1 ]; then
    set -- "$@" --proto '=https' --proto-redir '=https'
  fi
  curl "$@" --url "$url" -o "$out"
}

field() {
  printf '%s\n' "$manifest" | awk -v blk="$1" -v key="$2" '
    blk != "" && $0 == "  \"" blk "\": {" { inb = 1; next }
    inb && /^  },?$/ { inb = 0; next }
    {
      if (blk != "") {
        if (!inb) next
        prefix = "    \"" key "\": "
      } else {
        prefix = "  \"" key "\": "
      }
      if (index($0, prefix) != 1) next
      v = substr($0, length(prefix) + 1)
      sub(/,$/, "", v)
      gsub(/"/, "", v)
      print v
      exit
    }'
}

sha256_of() {
  case $hasher in
    sha256sum) sha256sum | awk '{ print $1 }' ;;
    shasum) shasum -a 256 | awk '{ print $1 }' ;;
    openssl) openssl dgst -sha256 | awk '{ print $NF }' ;;
  esac
}

main() {
  case $(uname -s) in
    Linux) os=linux ;;
    Darwin) os=macos ;;
    *) fail "unsupported OS: $(uname -s)" ;;
  esac
  case $(uname -m) in
    x86_64 | amd64) arch=x86_64 ;;
    aarch64 | arm64) arch=aarch64 ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
  target="$os-$arch"

  command -v curl >/dev/null 2>&1 || fail "curl is required"
  command -v awk >/dev/null 2>&1 || fail "awk is required"
  if command -v sha256sum >/dev/null 2>&1; then
    hasher=sha256sum
  elif command -v shasum >/dev/null 2>&1; then
    hasher=shasum
  elif command -v openssl >/dev/null 2>&1; then
    hasher=openssl
  else
    fail "sha256sum, shasum or openssl is required"
  fi

  want=${LOTEL_INSTALL_VERSION:-}
  if [ -n "$want" ] && ! valid_tag "$want"; then
    fail "LOTEL_INSTALL_VERSION must look like v1.2.3"
  fi
  base=${LOTEL_RELEASE_BASE_URL:-$default_base}
  while :; do
    case $base in
      */) base=${base%/} ;;
      *) break ;;
    esac
  done
  case $base in
    https://*) secure=1 ;;
    file://*) secure=0 ;;
    *) fail "LOTEL_RELEASE_BASE_URL must start with https:// or file://" ;;
  esac

  if [ -n "$want" ]; then
    manifest_url="$base/download/$want/lotel-manifest.json"
  else
    manifest_url="$base/latest/download/lotel-manifest.json"
  fi
  manifest=$(fetch 20 "$manifest_url" -) || fail "could not download $manifest_url"

  [ "$(field '' schema)" = 1 ] || fail "unsupported manifest schema"
  version=$(field '' version)
  valid_tag "v$version" || fail "manifest has an invalid version"
  [ "$(field '' tag)" = "v$version" ] || fail "manifest tag does not match its version"
  if [ -n "$want" ] && [ "v$version" != "$want" ]; then
    fail "manifest version v$version does not match requested $want"
  fi
  url=$(field assets "$target")
  digest=$(field sha256 "$target")
  [ -n "$url" ] || fail "target $target not found in manifest"
  [ "$url" = "$base/download/v$version/lotel-$target" ] || fail "unexpected asset URL for $target"
  case $digest in
    '' | *[!0123456789abcdef]*) fail "invalid sha256 for $target" ;;
  esac
  [ "${#digest}" -eq 64 ] || fail "invalid sha256 for $target"

  dir=${LOTEL_INSTALL_DIR:-${HOME:?HOME is not set}/.local/bin}
  case $dir in
    /*) ;;
    *) dir=$PWD/$dir ;;
  esac
  while :; do
    case $dir in
      ?*/) dir=${dir%/} ;;
      *) break ;;
    esac
  done
  mkdir -p "$dir" || fail "could not create $dir"
  [ ! -d "$dir/lotel" ] || fail "$dir/lotel is a directory"
  trap cleanup EXIT
  trap 'cleanup; exit 1' INT TERM HUP
  tmp=$(mktemp "$dir/.lotel-install.XXXXXX") || fail "could not create a temporary file in $dir"

  fetch 300 "$url" "$tmp" || fail "could not download $url"
  actual=$(sha256_of <"$tmp")
  [ "$actual" = "$digest" ] || fail "checksum mismatch for $target"

  chmod 0755 "$tmp"
  version_output=$(mktemp "$dir/.lotel-version.XXXXXX")
  sh -c 'ulimit -f 1; exec "$1" --version' sh "$tmp" >"$version_output" 2>/dev/null &
  version_pid=$!
  (sleep 10; kill -KILL "$version_pid" 2>/dev/null || true) >/dev/null 2>&1 &
  watchdog_pid=$!
  if wait "$version_pid"; then
    version_pid=
  else
    version_pid=
    fail "downloaded binary does not run on this system within 10 seconds"
  fi
  kill "$watchdog_pid" 2>/dev/null || true
  wait "$watchdog_pid" 2>/dev/null || true
  watchdog_pid=
  [ "$(wc -c <"$version_output")" -le 256 ] || fail "downloaded binary has invalid version output"
  reported=$(cat "$version_output")
  rm -f "$version_output"
  version_output=
  [ "$reported" = "lotel v$version" ] || fail "downloaded binary reports '$reported', expected 'lotel v$version'"

  mv -f "$tmp" "$dir/lotel"
  tmp=

  if [ -t 1 ]; then
    printf '\033[32mInstalled\033[0m lotel v%s to %s/lotel\n' "$version" "$dir"
  else
    printf 'Installed lotel v%s to %s/lotel\n' "$version" "$dir"
  fi

  case ":$PATH:" in
    *":$dir:"*) ;;
    *)
      printf 'warning: %s is not on your PATH. Add it with:\n' "$dir" >&2
      quoted_dir=$(printf '%sX' "$dir" | sed "s/'/'\\\\''/g")
      quoted_dir=${quoted_dir%X}
      printf "  export PATH='%s':\"\$%s\"\n" "$quoted_dir" PATH >&2
      ;;
  esac
  found=$(command -v lotel 2>/dev/null || true)
  if [ -n "$found" ] && [ "$found" != "$dir/lotel" ]; then
    printf 'warning: another lotel shadows this install: %s\n' "$found" >&2
  fi
}

main "$@"
