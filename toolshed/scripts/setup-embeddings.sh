#!/usr/bin/env bash
# setup-embeddings.sh — One-shot setup for local ONNX embedding support in ToolShed.
#
# Downloads:
#   1. libtokenizers   — HuggingFace tokenizers native library (daulet/tokenizers)
#   2. ONNX Runtime    — Microsoft ONNX Runtime shared library
#   3. Embedding model — model.onnx + tokenizer.json from HuggingFace
#
# Usage:
#   ./scripts/setup-embeddings.sh                                    # all defaults
#   ./scripts/setup-embeddings.sh jinaai/jina-embeddings-v3          # specify model repo
#   TOOLSHED_MODEL_DIR=./my-model ./scripts/setup-embeddings.sh      # custom output dir
#
# After running:
#   export TOOLSHED_MODEL_DIR=./models/jina-embeddings-v2-small-en
#   export TOOLSHED_ONNX_LIB=./lib/libonnxruntime.dylib   # or .so on Linux
#   CGO_LDFLAGS="-L./lib" go build ./cmd/ssh

set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────

TOKENIZERS_VERSION="${TOKENIZERS_VERSION:-v1.27.0}"
ONNXRUNTIME_VERSION="${ONNXRUNTIME_VERSION:-1.24.4}"
REPO="${1:-jinaai/jina-embeddings-v2-small-en}"
REPO_NAME="${REPO##*/}"
MODEL_DIR="${TOOLSHED_MODEL_DIR:-models/${REPO_NAME}}"
LIB_DIR="${TOOLSHED_LIB_DIR:-lib}"
HF_BASE="https://huggingface.co/${REPO}/resolve/main"

# ── Detect platform ─────────────────────────────────────────────────────────

OS="$(uname -s)"
ARCH="$(uname -m)"

case "${OS}" in
    Darwin) PLATFORM="darwin" ;;
    Linux)  PLATFORM="linux"  ;;
    *)      echo "error: unsupported OS: ${OS}"; exit 1 ;;
esac

case "${ARCH}" in
    arm64|aarch64) MACHINE="arm64" ; MACHINE_ALT="aarch64" ;;
    x86_64|amd64)  MACHINE="x86_64"; MACHINE_ALT="x86_64"  ;;
    *)             echo "error: unsupported architecture: ${ARCH}"; exit 1 ;;
esac

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║  ToolShed — Local Embeddings Setup                         ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""
echo "  Platform:  ${PLATFORM}/${MACHINE}"
echo "  Lib dir:   ${LIB_DIR}"
echo "  Model dir: ${MODEL_DIR}"
echo "  Model:     ${REPO}"
echo ""

mkdir -p "${LIB_DIR}" "${MODEL_DIR}"

# ── 1. libtokenizers (HuggingFace tokenizers native lib) ────────────────────

TOKENIZERS_ASSET="libtokenizers.${PLATFORM}-${MACHINE}.tar.gz"
TOKENIZERS_URL="https://github.com/daulet/tokenizers/releases/download/${TOKENIZERS_VERSION}/${TOKENIZERS_ASSET}"

if [ -f "${LIB_DIR}/libtokenizers.a" ]; then
    echo "[1/3] libtokenizers.a — already exists, skipping."
else
    echo "[1/3] Downloading libtokenizers (${TOKENIZERS_VERSION}, ${PLATFORM}/${MACHINE})..."
    if curl -fSL "${TOKENIZERS_URL}" | tar xz -C "${LIB_DIR}/"; then
        echo "  ✓ ${LIB_DIR}/libtokenizers.a"
    else
        # Try alternate arch name (aarch64 vs arm64)
        TOKENIZERS_ASSET_ALT="libtokenizers.${PLATFORM}-${MACHINE_ALT}.tar.gz"
        TOKENIZERS_URL_ALT="https://github.com/daulet/tokenizers/releases/download/${TOKENIZERS_VERSION}/${TOKENIZERS_ASSET_ALT}"
        echo "  Retrying with ${TOKENIZERS_ASSET_ALT}..."
        if curl -fSL "${TOKENIZERS_URL_ALT}" | tar xz -C "${LIB_DIR}/"; then
            echo "  ✓ ${LIB_DIR}/libtokenizers.a"
        else
            echo "  ✗ Failed to download libtokenizers."
            echo "    Check releases: https://github.com/daulet/tokenizers/releases"
            exit 1
        fi
    fi
fi

# ── 2. ONNX Runtime shared library ─────────────────────────────────────────

if [ "${PLATFORM}" = "darwin" ]; then
    ORT_LIB_NAME="libonnxruntime.dylib"
    ORT_ARCHIVE="onnxruntime-osx-${MACHINE}-${ONNXRUNTIME_VERSION}.tgz"
else
    ORT_LIB_NAME="libonnxruntime.so"
    # ONNX Runtime uses x64 not x86_64, aarch64 not arm64
    case "${MACHINE}" in
        x86_64) ORT_ARCH="x64" ;;
        arm64)  ORT_ARCH="aarch64" ;;
        *)      ORT_ARCH="${MACHINE}" ;;
    esac
    ORT_ARCHIVE="onnxruntime-linux-${ORT_ARCH}-${ONNXRUNTIME_VERSION}.tgz"
fi

ORT_URL="https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/${ORT_ARCHIVE}"

if [ -f "${LIB_DIR}/${ORT_LIB_NAME}" ]; then
    echo "[2/3] ${ORT_LIB_NAME} — already exists, skipping."
else
    echo "[2/3] Downloading ONNX Runtime (${ONNXRUNTIME_VERSION}, ${PLATFORM}/${MACHINE})..."
    TMPDIR_ORT="$(mktemp -d)"
    trap 'rm -rf "${TMPDIR_ORT}"' EXIT

    if curl -fSL "${ORT_URL}" | tar xz -C "${TMPDIR_ORT}"; then
        # The archive extracts to onnxruntime-*/lib/
        ORT_EXTRACTED="$(find "${TMPDIR_ORT}" -maxdepth 1 -type d -name 'onnxruntime-*' | head -1)"
        if [ -d "${ORT_EXTRACTED}/lib" ]; then
            cp "${ORT_EXTRACTED}/lib/${ORT_LIB_NAME}"* "${LIB_DIR}/" 2>/dev/null || true
            # Also grab the versioned symlink targets
            find "${ORT_EXTRACTED}/lib" -name "libonnxruntime*" -exec cp {} "${LIB_DIR}/" \;
            echo "  ✓ ${LIB_DIR}/${ORT_LIB_NAME}"
        else
            echo "  ✗ Could not find lib/ in extracted ONNX Runtime archive."
            exit 1
        fi
    else
        echo "  ✗ Failed to download ONNX Runtime."
        echo "    URL: ${ORT_URL}"
        echo "    Check releases: https://github.com/microsoft/onnxruntime/releases"
        exit 1
    fi
fi

# ── 3. Embedding model (ONNX + tokenizer) ──────────────────────────────────

if [ -f "${MODEL_DIR}/model.onnx" ] && [ -f "${MODEL_DIR}/tokenizer.json" ]; then
    echo "[3/3] Model files — already exist, skipping."
else
    echo "[3/3] Downloading model: ${REPO}..."

    # tokenizer.json
    if [ ! -f "${MODEL_DIR}/tokenizer.json" ]; then
        echo "  Downloading tokenizer.json..."
        if curl -fSL "${HF_BASE}/tokenizer.json" -o "${MODEL_DIR}/tokenizer.json"; then
            echo "  ✓ tokenizer.json"
        else
            echo "  ✗ Failed to download tokenizer.json"
            exit 1
        fi
    fi

    # model.onnx — try common paths
    if [ ! -f "${MODEL_DIR}/model.onnx" ]; then
        echo "  Downloading model.onnx..."
        ONNX_DOWNLOADED=false

        for ONNX_PATH in "onnx/model.onnx" "model.onnx"; do
            if curl -fSL "${HF_BASE}/${ONNX_PATH}" -o "${MODEL_DIR}/model.onnx" 2>/dev/null; then
                echo "  ✓ model.onnx (from ${ONNX_PATH})"
                ONNX_DOWNLOADED=true
                break
            fi
        done

        if [ "${ONNX_DOWNLOADED}" = false ]; then
            echo "  ✗ No pre-exported ONNX model found at ${REPO}."
            echo ""
            echo "  Export it locally with optimum:"
            echo ""
            echo "    pip install 'optimum[exporters]' onnx onnxruntime"
            echo "    optimum-cli export onnx --model ${REPO} ${MODEL_DIR}"
            echo ""
            exit 1
        fi
    fi
fi

# ── Summary ─────────────────────────────────────────────────────────────────

echo ""
echo "════════════════════════════════════════════════════════════════"
echo "  ✓ Setup complete!"
echo ""
echo "  Native libraries:"
ls -lh "${LIB_DIR}/"
echo ""
echo "  Model files:"
ls -lh "${MODEL_DIR}/"
echo ""
echo "  To build ToolShed with embeddings:"
echo ""
echo "    export CGO_LDFLAGS=\"-L./${LIB_DIR}\""
echo "    go build ./cmd/ssh"
echo ""
echo "  To run with semantic search:"
echo ""
echo "    export TOOLSHED_MODEL_DIR=./${MODEL_DIR}"
if [ "${PLATFORM}" = "darwin" ]; then
echo "    export TOOLSHED_ONNX_LIB=./${LIB_DIR}/libonnxruntime.dylib"
echo "    export DYLD_LIBRARY_PATH=./${LIB_DIR}:\${DYLD_LIBRARY_PATH:-}"
else
echo "    export TOOLSHED_ONNX_LIB=./${LIB_DIR}/libonnxruntime.so"
echo "    export LD_LIBRARY_PATH=./${LIB_DIR}:\${LD_LIBRARY_PATH:-}"
fi
echo ""
echo "════════════════════════════════════════════════════════════════"
