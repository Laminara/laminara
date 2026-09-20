#!/usr/bin/env bash
# Builds one operator's launcher from the JSON their server emits with
# `laminara-server client-config`. Everything the launcher needs — address,
# signing keys, hwid salt, branding — is baked in here, so a build belongs to
# exactly one server.
set -euo pipefail

cd "$(dirname "$0")"

usage() {
	cat >&2 <<'EOF'
usage: ./build-launcher.sh <client-config.json> [--target linux|windows|macos] [--name <файл>] [--out <каталог>]

  --target   для какой системы собирать (по умолчанию linux); macos — только на маке
  --name     как назвать готовый файл (по умолчанию — из брендинга конфига)
  --out      куда его положить (по умолчанию ./dist-launcher)
EOF
	exit 2
}

config=""
target="linux"
name=""
out="dist-launcher"

while [ $# -gt 0 ]; do
	case "$1" in
	--target)
		[ $# -ge 2 ] || usage
		target="$2"
		shift 2
		;;
	--name)
		[ $# -ge 2 ] || usage
		name="$2"
		shift 2
		;;
	--out)
		[ $# -ge 2 ] || usage
		out="$2"
		shift 2
		;;
	-h | --help) usage ;;
	-*) usage ;;
	*)
		[ -z "$config" ] || usage
		config="$1"
		shift
		;;
	esac
done

[ -n "$config" ] || usage
[ -f "$config" ] || {
	echo "нет файла конфигурации: $config" >&2
	exit 1
}
config="$(cd "$(dirname "$config")" && pwd)/$(basename "$config")"

case "$target" in
linux)
	rust_target=""
	suffix=""
	runner=(cargo build)
	;;
windows)
	rust_target="x86_64-pc-windows-msvc"
	suffix=".exe"
	command -v cargo-xwin >/dev/null || {
		echo "для сборки под Windows нужен cargo-xwin: cargo install cargo-xwin" >&2
		exit 1
	}
	runner=(cargo xwin build)
	;;
macos)
	rust_target=""
	suffix=""
	runner=(cargo build)
	[ "$(uname -s)" = "Darwin" ] || {
		echo "лаунчер для macOS собирается только на маке: нужны Xcode, lipo и codesign" >&2
		exit 1
	}
	command -v go >/dev/null || {
		echo "для пакета .app нужен Go: им собирается ../server/cmd/macbundle (перед этим — make generate)" >&2
		exit 1
	}
	;;
*)
	echo "неизвестная система: $target" >&2
	exit 2
	;;
esac

product="$(node -e '
const branding = (JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).branding) || {};
const chosen = branding.windowTitle || branding.name || "Laminara";
process.stdout.write(chosen.replace(/[\/\\:*?"<>|]/g, "").trim() || "Laminara");
' "$config")"
[ -n "$name" ] || name="$product"

echo "==> фронтенд"
pnpm build

echo "==> $target"
args=(--release --features custom-protocol --manifest-path src-tauri/Cargo.toml)
[ -n "$rust_target" ] && args+=(--target "$rust_target")
version="$(tr -d '[:space:]' < ../VERSION 2>/dev/null || echo 0.0.0)"
# The operator named their launcher once, in the server config. Patching it in
# here keeps that the only place: the window title, the file and the properties
# Windows shows all come from it.
tauri_config="$(node -e '
const branding = (JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).branding) || {};
process.stdout.write(JSON.stringify({
  productName: process.argv[2],
  bundle: { publisher: branding.name || process.argv[2] },
}));
' "$config" "$product")"

run_cargo() {
	TAURI_CONFIG="$tauri_config" LAMINARA_CLIENT_CONFIG="$config" "${runner[@]}" "$@"
}

mkdir -p "$out"

if [ "$target" = "macos" ]; then
	slices=()
	for apple in aarch64-apple-darwin x86_64-apple-darwin; do
		rustup target add "$apple" >/dev/null 2>&1 || true
		run_cargo "${args[@]}" --target "$apple"
		slices+=("target/$apple/release/laminara")
	done
	universal="target/release/laminara"
	mkdir -p target/release
	lipo -create -output "$universal" "${slices[@]}"
	codesign --force --sign - "$universal"
	codesign --verify --verbose "$universal"
	app="$(go run ../server/cmd/macbundle --config "$config" --binary "$universal" --name "$name" --version "$version" --out "$out")"
	codesign --force --deep --sign - "$app"
	codesign --verify --deep --strict "$app"
	tar -C "$out" -czf "$out/${name}.app.tar.gz" "$(basename "$app")"
	echo "==> $app"
	echo "==> $out/${name}.app.tar.gz"
	exit 0
fi

run_cargo "${args[@]}"

# productName renames the bundle, never the cargo binary.
built="target/${rust_target:+$rust_target/}release/laminara${suffix}"
[ -f "$built" ] || {
	echo "сборка прошла, но файла нет: $built" >&2
	exit 1
}

cp "$built" "$out/${name}${suffix}"
echo "==> $out/${name}${suffix}"
