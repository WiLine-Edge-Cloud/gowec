#!/bin/sh
# Installs gowec, the WEC cloud's terminal client:
#
#   curl -fsSL https://github.com/WiLine-Edge-Cloud/gowec/releases/latest/download/install.sh | sh
#
# It asks where to install (default /usr/local/bin, using sudo when that needs
# administrator rights). GOWEC_INSTALL_DIR answers that ahead of time, and
# GOWEC_VERSION=v0.1.0 installs that release instead of the latest one. With
# no terminal (CI) it uses /usr/local/bin if writable, else ~/.local/bin.
# Running it again updates gowec in place.
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

default=/usr/local/bin
dir="${GOWEC_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
	# With curl | sh the script arrives on stdin, so the question goes to the terminal.
	if { : </dev/tty; } 2>/dev/null; then
		printf "Install gowec in [%s]: " "$default" >/dev/tty
		read -r answer </dev/tty || answer=""
		dir="${answer:-$default}"
	elif [ -w "$default" ]; then
		dir="$default"
	else
		dir="$HOME/.local/bin"
	fi
fi
case "$dir" in "~"*) dir="$HOME${dir#\~}" ;; esac

# A folder you can't write to (like /usr/local/bin on most Macs) is written with sudo.
sudo=""
if ! { mkdir -p "$dir" && [ -w "$dir" ]; } 2>/dev/null; then
	command -v sudo >/dev/null 2>&1 ||
		fail "can't write to $dir; run again with GOWEC_INSTALL_DIR=\$HOME/.local/bin"
	echo "Writing to $dir needs administrator rights; sudo may ask for your password."
	sudo=sudo
	$sudo mkdir -p "$dir"
fi
$sudo install -m 0755 "$tmp/gowec" "$dir/gowec"

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
