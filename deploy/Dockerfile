# =============================================================================
# ToolShed — Fly.io Dockerfile
#
# Multi-stage build:
#   1. builder  — compiles Go binary with CGo (for HuggingFace tokenizers)
#   2. model    — downloads embedding model from HuggingFace (parallel)
#   3. runtime  — minimal image with Dolt + SSH server + ONNX Runtime + model
# =============================================================================

# =============================================================================
# Stage 1: builder
# Compiles the Go SSH server with CGo enabled (required for libtokenizers).
# =============================================================================
FROM golang:1.26-bookworm AS builder

# Download libtokenizers static library for CGo linking.
ARG TOKENIZERS_VERSION=v1.27.0
RUN curl -fSL "https://github.com/daulet/tokenizers/releases/download/${TOKENIZERS_VERSION}/libtokenizers.linux-amd64.tar.gz" \
    | tar xz -C /usr/local/lib/

WORKDIR /src

# Cache dependency downloads before copying the full source tree.
COPY go.mod go.sum ./
RUN go mod download

# Build the SSH server binary with CGo enabled.
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /toolshed-ssh ./cmd/ssh

# =============================================================================
# Stage 2: model
# Downloads the Jina v2 embedding model from HuggingFace.
# This runs in parallel with the builder stage.
# =============================================================================
FROM debian:bookworm-slim AS model

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ARG MODEL_REPO=jinaai/jina-embeddings-v2-small-en
RUN mkdir -p /model \
    && curl -fSL "https://huggingface.co/${MODEL_REPO}/resolve/main/tokenizer.json" \
       -o /model/tokenizer.json \
    && (curl -fSL "https://huggingface.co/${MODEL_REPO}/resolve/main/onnx/model.onnx" \
       -o /model/model.onnx 2>/dev/null \
    || curl -fSL "https://huggingface.co/${MODEL_REPO}/resolve/main/model.onnx" \
       -o /model/model.onnx)

# =============================================================================
# Stage 3: runtime
# Minimal image with Dolt, ONNX Runtime, the embedding model, and the
# ToolShed SSH server.
# =============================================================================
FROM debian:bookworm-slim

# Install runtime dependencies.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       ca-certificates \
       curl \
       bash \
       libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

# Install Dolt — MySQL-compatible database with Git-like versioning.
RUN curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | bash

# Install ONNX Runtime shared library (loaded at runtime via dlopen).
ARG ONNXRUNTIME_VERSION=1.24.4
RUN curl -fSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz" \
    | tar xz -C /tmp \
    && cp /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/libonnxruntime* /usr/local/lib/ \
    && rm -rf /tmp/onnxruntime-* \
    && ldconfig

# Copy the compiled SSH server binary from the builder stage.
COPY --from=builder /toolshed-ssh /usr/local/bin/toolshed-ssh

# Copy the embedding model from the model stage.
COPY --from=model /model/ /model/

# Copy database schema files used to initialise Dolt on first boot.
COPY schema/ /schema/

# Copy the static website (index.html, pitch.html, favicon, etc.)
COPY public/ /public/

# Copy the entrypoint script that starts Dolt and the SSH server.
COPY deploy/start.sh /start.sh
RUN chmod +x /start.sh

EXPOSE 2222 8080

CMD ["/start.sh"]
