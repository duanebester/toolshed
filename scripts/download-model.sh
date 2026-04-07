#!/usr/bin/env bash
# download-model.sh — Downloads a HuggingFace embedding model for local ONNX inference.
#
# Usage:
#   ./scripts/download-model.sh                          # default: jina-embeddings-v2-small-en
#   ./scripts/download-model.sh jinaai/jina-embeddings-v3
#   TOOLSHED_MODEL_DIR=./my-model ./scripts/download-model.sh
#
# Requirements:
#   - curl
#   - (optional) Python + optimum for models without pre-exported ONNX
#
# The script downloads model.onnx and tokenizer.json from HuggingFace.
# Set TOOLSHED_MODEL_DIR to control the output directory.

set -euo pipefail

REPO="${1:-jinaai/jina-embeddings-v2-small-en}"
REPO_NAME="${REPO##*/}"
MODEL_DIR="${TOOLSHED_MODEL_DIR:-models/${REPO_NAME}}"
HF_BASE="https://huggingface.co/${REPO}/resolve/main"

echo "==> Downloading model: ${REPO}"
echo "    Output directory:  ${MODEL_DIR}"
echo ""

mkdir -p "${MODEL_DIR}"

# --- tokenizer.json ---
echo "Downloading tokenizer.json..."
if curl -fSL "${HF_BASE}/tokenizer.json" -o "${MODEL_DIR}/tokenizer.json"; then
    echo "  ✓ tokenizer.json"
else
    echo "  ✗ Failed to download tokenizer.json"
    exit 1
fi

# --- model.onnx ---
# Try common ONNX paths on HuggingFace repos.
echo "Downloading model.onnx..."
ONNX_DOWNLOADED=false

for ONNX_PATH in "onnx/model.onnx" "model.onnx"; do
    if curl -fSL "${HF_BASE}/${ONNX_PATH}" -o "${MODEL_DIR}/model.onnx" 2>/dev/null; then
        echo "  ✓ model.onnx (from ${ONNX_PATH})"
        ONNX_DOWNLOADED=true
        break
    fi
done

if [ "${ONNX_DOWNLOADED}" = false ]; then
    echo "  ✗ No pre-exported ONNX model found on HuggingFace."
    echo ""
    echo "  You can export it locally with optimum:"
    echo ""
    echo "    pip install optimum[exporters] onnx onnxruntime"
    echo "    optimum-cli export onnx --model ${REPO} ${MODEL_DIR}"
    echo ""
    exit 1
fi

# --- Summary ---
echo ""
echo "==> Model ready at ${MODEL_DIR}/"
ls -lh "${MODEL_DIR}/"
echo ""
echo "To use with ToolShed:"
echo "  export TOOLSHED_MODEL_DIR=${MODEL_DIR}"
