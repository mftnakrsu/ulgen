#!/bin/sh
# install.sh — install the ulgen command-line tool on Linux and macOS.
#
# It finds the system and the processor, picks a release, downloads the
# archive and checksums.txt from a release page, checks the signature of
# checksums.txt when cosign is installed, checks the SHA-256 of the archive
# against checksums.txt, unpacks it and puts the ulgen binary into
# ULGEN_INSTALL_DIR (default $HOME/.local/bin), replacing an older one.
# With --deb it downloads the .deb package instead, checks it, leaves it in
# the current directory and prints the command to install it.
#
# Run it in one line:
#
#   curl -fsSL https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.sh | sh
#
# Environment variables:
#
#   ULGEN_VERSION       the release to install, for example 1.2.3 or v1.2.3
#   ULGEN_BASE_URL      where the release files live, by default
#                       https://github.com/mftnakrsu/ulgen/releases/download
#   ULGEN_INSTALL_DIR   where to put the binary, by default $HOME/.local/bin
#   ULGEN_TEST_UNAME_M  overrides uname -m; the tests set it, nothing else should
#   ULGEN_TEST_VERIFY   checks the signature also for a server on this machine;
#                       the tests set it, nothing else should
#
# The script never runs sudo and never writes outside ULGEN_INSTALL_DIR, a
# temporary directory of its own, and (for --deb) the current directory.

set -eu

# ---------------------------------------------------------------------------
# Messages. The language is Turkish when the first set, non-empty locale
# variable begins with "tr", English otherwise. Every message lives here, in
# the msg function, which takes a message name.
# ---------------------------------------------------------------------------

lang=en
locale_value=${LC_ALL:-}
if [ -z "$locale_value" ]; then
	locale_value=${LC_MESSAGES:-}
fi
if [ -z "$locale_value" ]; then
	locale_value=${LANG:-}
fi
case "$locale_value" in
tr*) lang=tr ;;
*) lang=en ;;
esac

msg() {
	msg_name=$1
	shift
	case "$msg_name" in
	usage)
		if [ "$lang" = tr ]; then
			printf 'Kullanım: install.sh [--deb] [--help]\n\nulgen programını bir sürüm arşivinden kurar.\nOrtam değişkenleri:\n  ULGEN_VERSION      kurulacak sürüm (örneğin 1.2.3 veya v1.2.3)\n  ULGEN_BASE_URL     sürüm dosyalarının bulunduğu yer\n  ULGEN_INSTALL_DIR  programın konacağı klasör (varsayılan $HOME/.local/bin)\n  ULGEN_TEST_UNAME_M uname -m yerine geçer (yalnızca testler kullanır)\n'
		else
			printf 'Usage: install.sh [--deb] [--help]\n\nInstalls the ulgen command-line tool from a release archive.\nEnvironment variables:\n  ULGEN_VERSION      the release to install (for example 1.2.3 or v1.2.3)\n  ULGEN_BASE_URL     where the release files live\n  ULGEN_INSTALL_DIR  where to put the binary (default $HOME/.local/bin)\n  ULGEN_TEST_UNAME_M overrides uname -m (the tests use it only)\n'
		fi
		;;
	os_unsupported)
		if [ "$lang" = tr ]; then
			printf 'desteklenmeyen işletim sistemi: %s\n' "$@"
		else
			printf 'unsupported operating system: %s\n' "$@"
		fi
		;;
	arch_unsupported)
		if [ "$lang" = tr ]; then
			printf 'desteklenmeyen işlemci: %s\n' "$@"
		else
			printf 'unsupported processor: %s\n' "$@"
		fi
		;;
	deb_needs_linux)
		if [ "$lang" = tr ]; then
			printf 'bu seçenek (--deb) yalnızca Linux üzerinde çalışır\n'
		else
			printf 'this option (--deb) works only on Linux\n'
		fi
		;;
	version_bad)
		if [ "$lang" = tr ]; then
			printf 'sürüm reddedildi "%s": sürüm bir rakamla başlar ve rakam ile harflerden oluşur\n' "$@"
		else
			printf 'refused version "%s": a version starts with a digit and holds digits and letters\n' "$@"
		fi
		;;
	release_unknown)
		if [ "$lang" = tr ]; then
			printf 'hiçbir sürüm bulunamadı; ULGEN_VERSION ile bir sürüm belirtin\n'
		else
			printf 'no release was found; set ULGEN_VERSION to name one\n'
		fi
		;;
	mirror_needs_version)
		if [ "$lang" = tr ]; then
			printf 'bir yansı (ULGEN_BASE_URL) için ULGEN_VERSION gerekir\n'
		else
			printf 'a mirror (ULGEN_BASE_URL) needs ULGEN_VERSION\n'
		fi
		;;
	base_url_bad)
		if [ "$lang" = tr ]; then
			printf 'adres reddedildi "%s": https, ya da http://127.0.0.1 veya http://localhost kullanın\n' "$@"
		else
			printf 'refused address "%s": use https, or http://127.0.0.1 or http://localhost\n' "$@"
		fi
		;;
	no_download_tool)
		if [ "$lang" = tr ]; then
			printf 'curl da wget de bulunamadı\n'
		else
			printf 'neither curl nor wget was found\n'
		fi
		;;
	no_sha_tool)
		if [ "$lang" = tr ]; then
			printf 'SHA-256 hesaplayacak araç bulunamadı (sha256sum, shasum veya openssl)\n'
		else
			printf 'no tool to compute SHA-256 was found (sha256sum, shasum or openssl)\n'
		fi
		;;
	download_failed)
		if [ "$lang" = tr ]; then
			printf '%s indirilemedi\n' "$@"
		else
			printf 'could not download %s\n' "$@"
		fi
		;;
	checksum_missing)
		if [ "$lang" = tr ]; then
			printf '%s checksums.txt içinde yok\n' "$@"
		else
			printf '%s is not listed in checksums.txt\n' "$@"
		fi
		;;
	signature_missing)
		if [ "$lang" = tr ]; then
			printf 'bu sürümde checksums.txt dosyasının imzası yok; hiçbir şey kurulmadı\n'
		else
			printf 'this release has no signature for checksums.txt; nothing was installed\n'
		fi
		;;
	signature_bad)
		if [ "$lang" = tr ]; then
			printf 'checksums.txt dosyasının imzası doğrulanamadı; hiçbir şey kurulmadı\n'
		else
			printf 'the signature of checksums.txt could not be verified; nothing was installed\n'
		fi
		;;
	signature_ok)
		if [ "$lang" = tr ]; then
			printf 'checksums.txt dosyasının imzası doğrulandı (cosign)\n'
		else
			printf 'the signature of checksums.txt was verified (cosign)\n'
		fi
		;;
	signature_unchecked)
		if [ "$lang" = tr ]; then
			printf 'not: cosign kurulu olmadığı için imza denetlenmedi; sağlama toplamı denetleniyor\n'
		else
			printf 'note: the signature was not checked because cosign is not installed; the checksum is checked\n'
		fi
		;;
	checksum_mismatch)
		if [ "$lang" = tr ]; then
			printf '%s sağlama toplamı checksums.txt ile uyuşmuyor\n' "$@"
		else
			printf 'the checksum of %s does not match checksums.txt\n' "$@"
		fi
		;;
	extract_failed)
		if [ "$lang" = tr ]; then
			printf '%s açılamadı\n' "$@"
		else
			printf 'could not unpack %s\n' "$@"
		fi
		;;
	binary_missing)
		if [ "$lang" = tr ]; then
			printf 'arşivde ulgen programı yok\n'
		else
			printf 'the archive did not contain the ulgen binary\n'
		fi
		;;
	installed)
		if [ "$lang" = tr ]; then
			printf 'ulgen şuraya kuruldu: %s\n' "$@"
		else
			printf 'ulgen was installed in %s\n' "$@"
		fi
		;;
	binary_version)
		if [ "$lang" = tr ]; then
			printf 'sürüm: %s\n' "$@"
		else
			printf 'version: %s\n' "$@"
		fi
		;;
	path_hint)
		if [ "$lang" = tr ]; then
			printf 'bu satırı kabuk profilinize ekleyin:\n  export PATH="%s:$PATH"\n' "$@"
		else
			printf 'add this line to your shell profile:\n  export PATH="%s:$PATH"\n' "$@"
		fi
		;;
	deb_done)
		if [ "$lang" = tr ]; then
			printf 'paketi kurmak için şunu çalıştırın:\n  sudo apt install ./%s\n' "$@"
		else
			printf 'run this to install the package:\n  sudo apt install ./%s\n' "$@"
		fi
		;;
	*)
		if [ "$lang" = tr ]; then
			printf 'bilinmeyen ileti: %s\n' "$msg_name"
		else
			printf 'unknown message: %s\n' "$msg_name"
		fi
		;;
	esac
}

say() { msg "$@"; }

err() {
	printf 'ulgen-install: ' >&2
	msg "$@" >&2
}

have() { command -v "$1" >/dev/null 2>&1; }

# ---------------------------------------------------------------------------
# main does the work. Nothing runs until the last line calls it, so a
# truncated download (the rest of the file missing) runs nothing at all.
# ---------------------------------------------------------------------------

main() {

# ---------------------------------------------------------------------------
# Options.
# ---------------------------------------------------------------------------

deb=0
for arg in "$@"; do
	case "$arg" in
	--deb) deb=1 ;;
	--help)
		say usage
		exit 0
		;;
	*)
		err usage
		exit 2
		;;
	esac
done

# ---------------------------------------------------------------------------
# System and processor.
# ---------------------------------------------------------------------------

sys=$(uname -s)
case "$sys" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*)
	err os_unsupported "$sys"
	exit 1
	;;
esac

machine=${ULGEN_TEST_UNAME_M:-$(uname -m)}
case "$machine" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	err arch_unsupported "$machine"
	exit 1
	;;
esac

if [ "$deb" = 1 ] && [ "$os" != linux ]; then
	err deb_needs_linux
	exit 1
fi

# ---------------------------------------------------------------------------
# Address of the release page.
# ---------------------------------------------------------------------------

if [ -n "${ULGEN_BASE_URL:-}" ]; then
	base=$ULGEN_BASE_URL
	base_was_set=1
else
	base=https://github.com/mftnakrsu/ulgen/releases/download
	base_was_set=0
fi

base_ok=0
base_is_loopback=0
case "$base" in
https://*) base_ok=1 ;;
esac
if [ "$base_ok" = 0 ]; then
	# A loopback mirror may use plain http, but only as
	# http://localhost:PORT or http://127.0.0.1:PORT (a port of digits,
	# then a slash or the end). A userinfo part such as
	# http://localhost:x@evil.com/ names another host and is refused.
	rest=${base#http://}
	if [ "$rest" != "$base" ]; then
		host_port=${rest%%/*}
		case "$host_port" in
		127.0.0.1:* | localhost:*)
			port=${host_port#*:}
			case "$port" in
			'' | *[!0-9]*) ;;
			*)
				base_ok=1
				base_is_loopback=1
				;;
			esac
			;;
		esac
	fi
fi
if [ "$base_ok" = 0 ]; then
	err base_url_bad "$base"
	exit 1
fi

# ---------------------------------------------------------------------------
# The version.
# ---------------------------------------------------------------------------

version=${ULGEN_VERSION:-}
if [ -n "$version" ]; then
	case "$version" in
	v*) version=${version#v} ;;
	esac
fi

tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ -z "$version" ]; then
	if [ "$base_was_set" = 1 ]; then
		err mirror_needs_version
		exit 1
	fi
	# Ask for the releases/latest page without following the redirect and
	# read the Location header; it ends with /tag/v<version>.
	header_file=$tmp/headers
	latest_url=https://github.com/mftnakrsu/ulgen/releases/latest
	if have curl; then
		curl -fsS --proto '=https' --proto-redir '=https' --max-time 300 -o /dev/null -D "$header_file" "$latest_url" || true
	elif have wget; then
		wget -q -T 300 --spider --server-response "$latest_url" 2>"$header_file" || true
	else
		err no_download_tool
		exit 1
	fi
	location=
	while IFS= read -r header_line; do
		case "$header_line" in
		*[Ll]ocation:*)
			location=${header_line#*:}
			break
			;;
		esac
	done <"$header_file"
	case "$location" in
	*"/tag/v"*)
		version=${location##*/tag/v}
		version=$(printf '%s' "$version" | tr -d ' \r\t')
		;;
	esac
	if [ -z "$version" ]; then
		err release_unknown
		exit 1
	fi
fi

case "$version" in
[0-9]*) ;;
*)
	err version_bad "$version"
	exit 1
	;;
esac
case "$version" in
*[!0-9A-Za-z.+~-]*)
	err version_bad "$version"
	exit 1
	;;
esac

# ---------------------------------------------------------------------------
# A tool to compute SHA-256; the check is never skipped.
# ---------------------------------------------------------------------------

if have sha256sum; then
	sha_tool=sha256sum
elif have shasum; then
	sha_tool=shasum
elif have openssl; then
	sha_tool=openssl
else
	err no_sha_tool
	exit 1
fi

# ---------------------------------------------------------------------------
# Downloading.
# ---------------------------------------------------------------------------

download() {
	url=$1
	dest=$2
	if have curl; then
		if [ "$base_is_loopback" = 1 ]; then
			curl -fsSL --proto '=https,http' --max-time 300 -o "$dest" "$url"
		else
			curl -fsSL --proto '=https' --proto-redir '=https' --max-time 300 -o "$dest" "$url"
		fi
	elif have wget; then
		wget -q -T 300 -O "$dest" "$url"
	else
		err no_download_tool
		exit 1
	fi
}

if [ "$deb" = 1 ]; then
	name=ulgen_${version}_${arch}.deb
else
	name=ulgen_${version}_${os}_${arch}.tar.gz
fi
archive=$tmp/$name
release_url=$base/v$version

if ! download "$release_url/$name" "$archive"; then
	err download_failed "$name"
	exit 1
fi
if ! download "$release_url/checksums.txt" "$tmp/checksums.txt"; then
	err download_failed checksums.txt
	exit 1
fi

# ---------------------------------------------------------------------------
# The signature of checksums.txt. The release workflow signs the file with
# cosign, with the workflow's own identity and no key of ours. With cosign
# installed the signature must be there and must be this repository's
# release workflow's; without cosign the install goes on with the checksum
# alone and says so. A server on this machine (a mirror, a test) is not asked
# for a signature.
# ---------------------------------------------------------------------------

if [ "$base_is_loopback" = 0 ] || [ "${ULGEN_TEST_VERIFY:-}" = 1 ]; then
	if have cosign; then
		bundle=$tmp/checksums.txt.sigstore.json
		if ! download "$release_url/checksums.txt.sigstore.json" "$bundle" 2>/dev/null; then
			err signature_missing
			exit 1
		fi
		if ! cosign verify-blob \
			--bundle "$bundle" \
			--certificate-oidc-issuer https://token.actions.githubusercontent.com \
			--certificate-identity-regexp '^https://github\.com/mftnakrsu/ulgen/\.github/workflows/release\.yml@refs/tags/v' \
			"$tmp/checksums.txt" >/dev/null 2>&1; then
			err signature_bad
			exit 1
		fi
		say signature_ok
	else
		say signature_unchecked
	fi
fi

# ---------------------------------------------------------------------------
# The SHA-256 check, before anything is unpacked.
# ---------------------------------------------------------------------------

case "$sha_tool" in
sha256sum)
	actual=$(sha256sum "$archive")
	actual=${actual%% *}
	;;
shasum)
	actual=$(shasum -a 256 "$archive")
	actual=${actual%% *}
	;;
openssl)
	actual=$(openssl dgst -sha256 "$archive")
	actual=${actual##*= }
	;;
esac

expected=
while IFS= read -r line || [ -n "$line" ]; do
	line_hash=${line%% *}
	line_rest=${line#* }
	line_rest=${line_rest# }
	line_rest=${line_rest# }
	if [ "$line_rest" = "$name" ]; then
		expected=$line_hash
		break
	fi
done <"$tmp/checksums.txt"

if [ -z "$expected" ]; then
	err checksum_missing "$name"
	exit 1
fi
if [ "$expected" != "$actual" ]; then
	err checksum_mismatch "$name"
	exit 1
fi

# ---------------------------------------------------------------------------
# The .deb package: leave it in the current directory.
# ---------------------------------------------------------------------------

if [ "$deb" = 1 ]; then
	mv "$archive" "./$name"
	say deb_done "$name"
	exit 0
fi

# ---------------------------------------------------------------------------
# Unpack and install the binary.
# ---------------------------------------------------------------------------

if ! tar -xzf "$archive" -C "$tmp"; then
	err extract_failed "$name"
	exit 1
fi

if [ ! -f "$tmp/ulgen" ]; then
	err binary_missing
	exit 1
fi

if [ -n "${ULGEN_INSTALL_DIR:-}" ]; then
	dir=$ULGEN_INSTALL_DIR
else
	dir=$HOME/.local/bin
fi

mkdir -p "$dir"
staged=$dir/.ulgen.tmp.$$
cp "$tmp/ulgen" "$staged"
chmod 755 "$staged"
mv -f "$staged" "$dir/ulgen"

say installed "$dir"
version_text=$("$dir/ulgen" --version 2>/dev/null) || version_text=""
if [ -n "$version_text" ]; then
	say binary_version "$version_text"
fi

case ":$PATH:" in
*":$dir:"*) ;;
*) say path_hint "$dir" ;;
esac

exit 0
}

main "$@"
