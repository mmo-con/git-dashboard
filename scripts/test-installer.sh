#!/bin/sh
set -eu

version=v9.8.7
tmp=$(mktemp -d "${TMPDIR:-/tmp}/headquarters-installer-test.XXXXXXXX")
trap 'find "$tmp" -depth -delete' 0
installer=$tmp/install.sh
make --no-print-directory installer VERSION="$version" > "$installer"
mock=$tmp/mock
assets=$tmp/assets
home=$tmp/home
work=$tmp/work
output=$tmp/output
binary=$home/.local/bin/headquarters
mkdir -p "$mock" "$assets" "$home" "$work"

real_tar=$(command -v tar)
export real_tar
cat > "$mock/tar" <<'EOF'
#!/bin/sh
touch "$EXTRACTED"
exec "$real_tar" "$@"
EOF
cat > "$mock/uname" <<'EOF'
#!/bin/sh
case "$1" in
    -s) printf '%s\n' "$TEST_OS" ;;
    -m) printf '%s\n' "$TEST_ARCH" ;;
    *) exit 1 ;;
esac
EOF
cat > "$mock/curl" <<'EOF'
#!/bin/sh
while [ "$#" -gt 2 ]; do shift; done
case "$2" in
    "$RELEASE_URL/"*) cp "$ASSETS/${2#"$RELEASE_URL/"}" "$1" ;;
    *) printf 'Unexpected URL: %s\n' "$2" >&2; exit 1 ;;
esac
EOF
chmod +x "$mock/curl" "$mock/uname" "$mock/tar"

run_installer() {
    TEST_OS=$1 TEST_ARCH=$2 PATH="$mock:$3" HOME="$home" TMPDIR="$work" \
        ASSETS="$assets" EXTRACTED="$tmp/extracted" \
        RELEASE_URL="https://github.com/mmo-con/headquarters/releases/download/$version" \
        sh "$installer" > "$output" 2>&1
}

fail() {
    printf 'Installer test failed: %s\n' "$*" >&2
    cat "$output" >&2
    exit 1
}

make_archive() {
    name=headquarters_"$version"_$1
    mkdir "$tmp/$name"
    printf '%s\n' "$1" > "$tmp/$name/headquarters"
    "$real_tar" -czf "$assets/$name.tar.gz" -C "$tmp" "$name"
}

for platform in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
    make_archive "$platform"
done
(
    cd "$assets"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum headquarters_*.tar.gz
    else
        shasum -a 256 headquarters_*.tar.gz
    fi
) > "$tmp/checksums"
cp "$tmp/checksums" "$assets/checksums.txt"

check_clean() {
    [ -z "$(ls -A "$work")" ] || fail 'temporary files remain'
}

success() {
    run_installer "$1" "$2" "$4" || fail "installing $1/$2"
    [ -x "$binary" ] && [ "$(cat "$binary")" = "$3" ] || fail "wrong or missing binary for $1/$2"
    check_clean
    printf 'ok - install %s/%s\n' "$1" "$2"
}

success Linux x86_64 linux_amd64 "$PATH"
grep -Fq 'export PATH="$HOME/.local/bin:$PATH"' "$output" || fail 'missing PATH hint'
success Linux aarch64 linux_arm64 "$PATH"
success Darwin x86_64 darwin_amd64 "$PATH"
success Darwin arm64 darwin_arm64 "$home/.local/bin:$PATH"
if grep -Fq 'export PATH=' "$output"; then fail 'unexpected PATH hint'; fi

failure() {
    rm -f "$tmp/extracted"
    if run_installer "$1" "$2" "$PATH"; then fail "unexpected success: $3"; fi
    grep -Fq "$3" "$output" || fail "missing error: $3"
    [ ! -e "$tmp/extracted" ] || fail 'archive extracted before verification'
    [ "$(cat "$binary")" = darwin_arm64 ] || fail 'existing installation changed'
    check_clean
    printf 'ok - %s\n' "$3"
}

failure FreeBSD x86_64 'Unsupported operating system'
failure Linux i686 'Unsupported architecture'
archive=headquarters_"$version"_linux_amd64.tar.gz
mv "$assets/$archive" "$tmp/$archive"
failure Linux x86_64 "Could not download $archive"
mv "$tmp/$archive" "$assets/$archive"
rm "$assets/checksums.txt"
failure Linux x86_64 'Could not download checksums.txt'
printf 'invalid  %s.extra\n' "$archive" > "$assets/checksums.txt"
failure Linux x86_64 'Expected exactly one valid SHA-256 entry'
printf 'invalid  %s\n' "$archive" > "$assets/checksums.txt"
failure Linux x86_64 'Expected exactly one valid SHA-256 entry'
cat "$tmp/checksums" "$tmp/checksums" > "$assets/checksums.txt"
failure Linux x86_64 'Expected exactly one valid SHA-256 entry'
printf '%064d  %s\n' 0 "$archive" > "$assets/checksums.txt"
failure Linux x86_64 'SHA-256 verification failed'
