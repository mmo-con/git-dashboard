#!/bin/sh

version='@VERSION@'

die() {
    printf 'headquarters installer: %s\n' "$*" >&2
    exit 1
}

case "$version" in
    ''|*[!a-zA-Z0-9._-]*) die 'Use install.sh from a published headquarters release.' ;;
esac

system=$(uname -s) || die 'Could not detect the operating system.'
machine=$(uname -m) || die 'Could not detect the architecture.'
case "$system" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) die "Unsupported operating system: $system (expected Linux or macOS)." ;;
esac
case "$machine" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) die "Unsupported architecture: $machine (expected amd64 or arm64)." ;;
esac

[ -n "${HOME:-}" ] || die 'HOME is not set.'
command -v curl >/dev/null 2>&1 || die 'curl is required.'

name=headquarters_${version}_${os}_${arch}
archive=$name.tar.gz
base=https://github.com/mmo-con/headquarters/releases/download/$version
bindir=$HOME/.local/bin
work=$(mktemp -d "${TMPDIR:-/tmp}/headquarters-install.XXXXXXXX") ||
    die 'Could not create a temporary directory.'
staged=
cleanup() {
    if [ -n "$staged" ]; then rm -f "$staged"; fi
    rm -f "$work/$archive" "$work/checksums.txt" "$work/headquarters"
    rmdir "$work"
}
trap cleanup 0
trap 'exit 1' HUP INT TERM

download() {
    curl -q -fsSL --proto '=https' --proto-redir '=https' -o "$work/$1" "$base/$1" ||
        die "Could not download $1 from release $version."
}
download "$archive"
download checksums.txt

checksum=$(LC_ALL=C awk -v file="$archive" '
    $2 == file || $2 == "*" file {
        count++
        if (NF == 2 && length($1) == 64 && $1 ~ /^[[:xdigit:]]+$/) hash = $1
    }
    END {
        if (count != 1 || hash == "") exit 1
        print hash
    }
' "$work/checksums.txt") || die "Expected exactly one valid SHA-256 entry for $archive."

(
    cd "$work" || exit 1
    if command -v sha256sum >/dev/null 2>&1; then
        printf '%s  %s\n' "$checksum" "$archive" | sha256sum -c -
    elif command -v shasum >/dev/null 2>&1; then
        printf '%s  %s\n' "$checksum" "$archive" | shasum -a 256 -c -
    else
        die 'sha256sum or shasum is required.'
    fi
) || die "SHA-256 verification failed for $archive."

tar -xOzf "$work/$archive" "$name/headquarters" > "$work/headquarters" &&
    [ -s "$work/headquarters" ] || die "Could not extract headquarters from $archive."

mkdir -p "$bindir" || die "Could not create $bindir."
[ ! -d "$bindir/headquarters" ] || die "$bindir/headquarters is a directory."
# Stage next to the destination so replacement is a rename on the same filesystem.
staged=$(mktemp "$bindir/.headquarters.XXXXXXXX") || die "Could not stage the binary in $bindir."
install -m 755 "$work/headquarters" "$staged" &&
    mv -f "$staged" "$bindir/headquarters" || die "Could not install headquarters in $bindir."
staged=

printf 'Installed headquarters %s to %s/headquarters\n' "$version" "$bindir"
case ":$PATH:" in
    *":$bindir:"*) ;;
    *) printf '%s\n' "$bindir is not on PATH. Add it in your shell:" \
        '  export PATH="$HOME/.local/bin:$PATH"' ;;
esac
