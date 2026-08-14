#!/bin/sh
set -eu

version="${1:-0.1.0}"
output_directory="${2:-dist/pactline}"
project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

case "$version" in
  *[!0-9A-Za-z.-]*|.*|*..*|*.)
    echo "invalid release version: $version" >&2
    exit 2
    ;;
esac

mkdir -p "$output_directory"
output_directory=$(CDPATH= cd -- "$output_directory" && pwd)
temporary_directory=$(mktemp -d)
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM

for target in darwin_amd64 darwin_arm64 linux_amd64 linux_arm64; do
  os=${target%_*}
  arch=${target#*_}
  package="pactline_${version}_${os}_${arch}"
  package_directory="$temporary_directory/$package"
  mkdir -p "$package_directory"
  (
    cd "$project_root"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
      -trimpath -ldflags="-s -w -X github.com/wolfhead/pactline/internal/cli.Version=$version" \
      -o "$package_directory/pactline" ./cmd/pactline
  )
  cp "$project_root/cmd/pactline/README.md" "$package_directory/README.md"
  cp "$project_root/LICENSE" "$package_directory/LICENSE"
  tar -C "$temporary_directory" -czf "$output_directory/$package.tar.gz" "$package"
done

(
  cd "$output_directory"
  shasum -a 256 pactline_"$version"_*.tar.gz > pactline_checksums.txt
)
