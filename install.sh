#!/bin/sh
# Installs dn, the terminal client for /dev/null, from the latest GitHub release, checking it against the release's
# checksums.txt first.
#
#   curl -fsSL https://raw.githubusercontent.com/eggplants/dev-null-tui/master/install.sh | sh
#
# DN_VERSION picks a release (like 0.0.4) and DN_INSTALL_DIR where dn goes (~/.local/bin by default).
set -eu

repo=eggplants/dev-null-tui
dir=${DN_INSTALL_DIR:-$HOME/.local/bin}

fail() {
  echo "install.sh: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "this needs $1"
}

need curl
need tar
need uname

case $(uname -s) in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "no release for $(uname -s); try go install github.com/$repo/cmd/dn@latest" ;;
esac

case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "no release for $(uname -m)" ;;
esac

version=${DN_VERSION:-}
if [ -z "$version" ]; then
  # The latest release's page redirects to its tag.
  tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
  version=${tag##*/v}
fi
version=${version#v}
[ -n "$version" ] || fail "couldn't find the latest release"

archive="dev-null-tui_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/v$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading dn $version for $os/$arch..."
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d ' ' -f 1)
[ -n "$want" ] || fail "$archive isn't in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$archive" | cut -d ' ' -f 1)
elif command -v shasum >/dev/null 2>&1; then
  got=$(shasum -a 256 "$tmp/$archive" | cut -d ' ' -f 1)
else
  fail "this needs sha256sum or shasum to check the download"
fi
[ "$got" = "$want" ] || fail "$archive's checksum is $got, not $want"

tar -xzf "$tmp/$archive" -C "$tmp" dn
mkdir -p "$dir"
mv "$tmp/dn" "$dir/dn"
chmod 755 "$dir/dn"

echo "Installed $dir/dn"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "$dir isn't on your PATH; add it, or run $dir/dn" ;;
esac
echo "Run dn login to sign in."
