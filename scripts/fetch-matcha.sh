#!/usr/bin/env bash
# 下载 Matcha 中文模型 + vocoder（约 140MB）。
# 用法: ./scripts/fetch-matcha.sh [目标目录]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${1:-$ROOT/models}"
TTS="https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models"
VOC="https://github.com/k2-fsa/sherpa-onnx/releases/download/vocoder-models"

mkdir -p "$DEST"
if [ ! -d "$DEST/matcha-icefall-zh-baker" ]; then
  echo "下载 matcha-icefall-zh-baker"
  curl -fsSL --retry 3 -o "$DEST/matcha.tar.bz2" "$TTS/matcha-icefall-zh-baker.tar.bz2"
  tar xjf "$DEST/matcha.tar.bz2" -C "$DEST"
  rm -f "$DEST/matcha.tar.bz2"
fi
if [ ! -f "$DEST/vocos-22khz-univ.onnx" ]; then
  echo "下载 vocos-22khz-univ.onnx"
  curl -fsSL --retry 3 -o "$DEST/vocos-22khz-univ.onnx" "$VOC/vocos-22khz-univ.onnx"
fi
echo "完成:"
du -sh "$DEST/matcha-icefall-zh-baker" "$DEST/vocos-22khz-univ.onnx"
