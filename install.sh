#!/bin/sh
# Installs gowec, the WEC cloud's terminal client:
#
#   curl -fsSL https://github.com/WiLine-Edge-Cloud/gowec/releases/latest/download/install.sh | sh
#
# GOWEC_VERSION=v0.1.0 installs that release instead of the latest one, and
# GOWEC_INSTALL_DIR picks the folder (default /usr/local/bin when writable,
# else ~/.local/bin). Running it again updates gowec in place.
set -eu

BASE_URL="${GOWEC_BASE_URL:-https://github.com/WiLine-Edge-Cloud/gowec/releases}"
VERSION="${GOWEC_VERSION:-latest}"
# GitHub serves the newest release at latest/download/ and others at download/<tag>/.
if [ "$VERSION" = latest ]; then
	files="$BASE_URL/latest/download"
else
	files="$BASE_URL/download/$VERSION"
fi

fail() {
	echo "gowec: $*" >&2
	exit 1
}

case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "no installer for $(uname -s); on Windows, download $BASE_URL/latest/download/gowec_windows_amd64.zip" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "no build for $(uname -m)" ;;
esac

archive="gowec_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading gowec ($VERSION, $os/$arch)…"
curl -fsSL --retry 3 "$files/$archive" -o "$tmp/$archive" ||
	fail "can't download $files/$archive"
curl -fsSL --retry 3 "$files/checksums.txt" -o "$tmp/checksums.txt" ||
	fail "can't download $files/checksums.txt"

want="$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
[ -n "$want" ] || fail "$archive isn't listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	got="$(sha256sum "$tmp/$archive" | cut -d' ' -f1)"
else
	got="$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)"
fi
[ "$want" = "$got" ] || fail "the download doesn't match its checksum; nothing was installed"

tar -xzf "$tmp/$archive" -C "$tmp" gowec

dir="${GOWEC_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
fi
mkdir -p "$dir"
install -m 0755 "$tmp/gowec" "$dir/gowec"

echo "Installed $("$dir/gowec" version) in $dir"
case ":$PATH:" in
*":$dir:"*) echo "Run gowec to open the console." ;;
*)
	rc="$HOME/.profile"
	case "${SHELL:-}" in
	*/zsh) rc="$HOME/.zshrc" ;;
	*/bash) rc="$HOME/.bashrc" ;;
	esac
	# Shown with $HOME and ~ so the line can be copied as is.
	show_dir="$dir"
	case "$dir" in "$HOME"/*) show_dir="\$HOME${dir#"$HOME"}" ;; esac
	show_rc="~${rc#"$HOME"}"
	echo "$dir isn't on your PATH yet. Add it, then run gowec:"
	echo "  echo 'export PATH=\"$show_dir:\$PATH\"' >> $show_rc && source $show_rc"
	;;
esac
