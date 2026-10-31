#!/usr/bin/env bash
# 下载 Piper 模型到 models/。
# 用法: ./scripts/fetch-models.sh [目标目录] [模型名...]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${1:-$ROOT/models}"
shift || true
NAMES=("$@")
if [ ${#NAMES[@]} -eq 0 ]; then
  NAMES=(vits-piper-zh_CN-huayan-medium vits-piper-en_US-lessac-medium)
fi
BASE="https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models"

mkdir -p "$DEST"
for name in "${NAMES[@]}"; do
  if [ -d "$DEST/$name" ]; then
    echo "已存在，跳过: $name"
    continue
  fi
  echo "下载 $name"
  curl -fsSL --retry 3 -o "$DEST/$name.tar.bz2" "$BASE/$name.tar.bz2"
  tar xjf "$DEST/$name.tar.bz2" -C "$DEST"
  rm -f "$DEST/$name.tar.bz2"
done
echo "完成:"
ls -1 "$DEST"
