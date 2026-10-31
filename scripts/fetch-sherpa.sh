#!/usr/bin/env bash
# 下载 sherpa-onnx 预编译共享库。
# 用法: ./scripts/fetch-sherpa.sh [版本] [目标目录]
set -euo pipefail

VERSION="${1:-v1.13.8}"
DEST="${2:-$(cd "$(dirname "$0")/.." && pwd)/vendor/sherpa-onnx}"

case "$(uname -m)" in
  # aarch64 的 CPU-only 构建发布的资产名带 -shared-cpu
  aarch64 | arm64) SUFFIX=linux-aarch64-shared-cpu ;;
  x86_64 | amd64) SUFFIX=linux-x64-shared ;;
  armv7l | armv6l) SUFFIX=linux-arm-gnueabihf-shared ;;
  *) echo "不支持的架构: $(uname -m)" >&2; exit 1 ;;
esac

NAME="sherpa-onnx-${VERSION}-${SUFFIX}"
URL="https://github.com/k2-fsa/sherpa-onnx/releases/download/${VERSION}/${NAME}.tar.bz2"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "下载 $URL"
curl -fsSL --retry 3 -o "$TMP/pkg.tar.bz2" "$URL"
mkdir -p "$DEST"
tar xjf "$TMP/pkg.tar.bz2" -C "$TMP"
cp -a "$TMP/${NAME}/." "$DEST/"
echo "完成: $(ls "$DEST/lib" | tr '\n' ' ')"
