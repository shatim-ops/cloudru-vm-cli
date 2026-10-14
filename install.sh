#!/bin/sh
# Установка cloudru-vm из GitHub Releases (Linux, macOS).
#
#   curl -fsSL https://raw.githubusercontent.com/shatim-ops/cloudru-vm-cli/main/install.sh | sh
#
# Переменные окружения:
#   CLOUDRU_VM_VERSION  тег релиза, например v1.0.0-rc.1 (по умолчанию —
#                       последний стабильный; pre-release надо назвать явно)
#   INSTALL_DIR         куда положить бинарь (по умолчанию /usr/local/bin
#                       для root, иначе ~/.local/bin)
#   CLOUDRU_VM_BASE_URL откуда качать вместо GitHub (зеркало, проверка
#                       скрипта); ожидается <base>/<тег>/<файл>
#
# Скрипт сверяет sha256 архива с checksums.txt релиза. Если в PATH есть
# cosign, он же проверяет подпись checksums.txt (keyless, identity —
# release.yml этого репозитория). Windows: см. README, раздел «Установка».
set -eu

REPO="shatim-ops/cloudru-vm-cli"
BINARY="cloudru-vm"

say() { printf '%s\n' "$*" >&2; }
die() { say "install.sh: $*"; exit 1; }

need() {
	command -v "$1" >/dev/null 2>&1 || die "нужна утилита $1"
}

# Без cosign целостность держится на TLS: checksums.txt приходит оттуда же,
# откуда архив. Поэтому для https-адреса curl не идёт ни на что, кроме
# https, и в том числе по редиректам (GitHub отдаёт файлы релиза редиректом
# на другой хост): иначе «https → http» отдал бы архив и суммы тому, кто
# сидит в сети.
download() { # url file
	if command -v curl >/dev/null 2>&1; then
		case "$1" in
		https://*) curl --proto '=https' --tlsv1.2 -fsSL -o "$2" "$1" ;;
		*) curl -fsSL -o "$2" "$1" ;;
		esac
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "нужен curl или wget"
	fi
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) die "ОС $(uname -s) не поддерживается скриптом; для Windows — zip из релиза" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "архитектура $(uname -m) не поддерживается (есть amd64, arm64)" ;;
	esac
}

# Последний стабильный тег — из редиректа /releases/latest, без API и его
# лимитов. Pre-release (rc) туда не попадает.
latest_tag() {
	need curl
	url=$(curl --proto '=https' --tlsv1.2 -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
		die "не удалось узнать последний релиз; задайте CLOUDRU_VM_VERSION"
	tag=${url##*/}
	case "$tag" in
	v*) echo "$tag" ;;
	*) die "стабильного релиза пока нет; задайте CLOUDRU_VM_VERSION, например v1.0.0-rc.1" ;;
	esac
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "нужен sha256sum или shasum"
	fi
}

main() {
	need uname
	need tar
	os=$(detect_os)
	arch=$(detect_arch)
	tag=${CLOUDRU_VM_VERSION:-}
	[ -n "$tag" ] || tag=$(latest_tag)
	case "$tag" in v*) ;; *) tag="v$tag" ;; esac
	version=${tag#v}

	base=${CLOUDRU_VM_BASE_URL:-https://github.com/$REPO/releases/download}
	case "$base" in
	https://*) ;;
	*) say "внимание: $base — не https; без cosign подлинность архива не проверяется ничем" ;;
	esac
	archive="${BINARY}_${version}_${os}_${arch}.tar.gz"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "Скачиваю $archive ($tag)"
	download "$base/$tag/$archive" "$tmp/$archive" || die "не удалось скачать $base/$tag/$archive"
	download "$base/$tag/checksums.txt" "$tmp/checksums.txt" || die "не удалось скачать checksums.txt"

	if command -v cosign >/dev/null 2>&1; then
		download "$base/$tag/checksums.txt.sigstore.json" "$tmp/checksums.txt.sigstore.json" ||
			die "не удалось скачать подпись checksums.txt.sigstore.json"
		cosign verify-blob \
			--bundle "$tmp/checksums.txt.sigstore.json" \
			--certificate-identity "https://github.com/$REPO/.github/workflows/release.yml@refs/tags/$tag" \
			--certificate-oidc-issuer https://token.actions.githubusercontent.com \
			"$tmp/checksums.txt" >/dev/null 2>&1 || die "подпись checksums.txt не прошла проверку cosign"
		say "Подпись checksums.txt: OK (cosign)"
	else
		say "cosign не найден: подпись не проверена, только sha256"
	fi

	want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "$archive нет в checksums.txt"
	got=$(sha256_of "$tmp/$archive")
	[ "$want" = "$got" ] || die "sha256 не совпал: ожидался $want, получен $got"
	say "sha256: OK"

	tar -xzf "$tmp/$archive" -C "$tmp" "$BINARY"

	dir=${INSTALL_DIR:-}
	if [ -z "$dir" ]; then
		if [ "$(id -u)" -eq 0 ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
	fi
	mkdir -p "$dir"
	# Через временное имя: запущенный старый бинарь не ломается на середине.
	cp "$tmp/$BINARY" "$dir/.$BINARY.new"
	chmod 0755 "$dir/.$BINARY.new"
	mv -f "$dir/.$BINARY.new" "$dir/$BINARY"

	say "Установлено: $dir/$BINARY"
	case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "Каталога $dir нет в PATH. Добавьте в профиль оболочки: export PATH=\"$dir:\$PATH\"" ;;
	esac
	"$dir/$BINARY" version >&2 || true
}

main "$@"
