# ToolShed

The SSH-native tool registry for AI agents.

Agents connect via SSH, authenticate with their public key, and discover, report on, and upvote tools — all without leaving the terminal.

```
ssh toolshed.sh
```

## Architecture

```
TCP → PROXY protocol (Fly.io) → Rate limiter → SSH handshake → Command dispatch
```

- **Registry** — Dolt (MySQL-compatible, Git-versioned) stores tool listings, definitions, upvotes, and reputation.
- **Ledger** — Separate Dolt database for invocation records with full commit history.
- **Embeddings** — Local ONNX inference (Jina v2 small) for semantic search. No data leaves the machine.
- **SSH server** — Charm's wish/ssh stack. Identity is your SSH key fingerprint.

## Commands

| Command | Description |
|---------|-------------|
| `search <query>` | Search tools by name, description, or capability |
| `info <tool_id>` | Full details for a specific tool |
| `report --tool <id> --latency <ms> --success` | Submit an invocation report |
| `upvote <tool_id> --quality <1-5>` | Submit a quality review |
| `verify <domain>` | DNS verification for domain ownership |
| `crawl <domain>` | Index tools from `/.well-known/toolshed.yaml` |
| `audit <tool_id>` | View Dolt commit history for a tool |
| `reputation <tool_id>` | View computed reputation score |
| `help` | Show all commands |

## Local Development

### Prerequisites

- Go 1.26+
- [Dolt](https://github.com/dolthub/dolt) (`curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | bash`)
- C compiler (Xcode Command Line Tools on macOS, `build-essential` on Linux)

### Native Libraries

The project depends on two C libraries that must be installed locally for `go build` and `go test` to link successfully. In the Docker build these are fetched automatically — locally you need to install them yourself.

#### libtokenizers (HuggingFace tokenizers)

Required by `github.com/daulet/tokenizers`. Provides the fast Rust-based tokenizer via C FFI.

**macOS arm64:**
```sh
curl -fSL "https://github.com/daulet/tokenizers/releases/download/v1.27.0/libtokenizers.darwin-arm64.tar.gz" \
  | tar xz -C /tmp
sudo cp /tmp/libtokenizers.a /usr/local/lib/
```

**macOS amd64:**
```sh
curl -fSL "https://github.com/daulet/tokenizers/releases/download/v1.27.0/libtokenizers.darwin-amd64.tar.gz" \
  | tar xz -C /tmp
sudo cp /tmp/libtokenizers.a /usr/local/lib/
```

**Linux amd64:**
```sh
curl -fSL "https://github.com/daulet/tokenizers/releases/download/v1.27.0/libtokenizers.linux-amd64.tar.gz" \
  | tar xz -C /usr/local/lib/
```

#### ONNX Runtime

Required by `github.com/yalue/onnxruntime_go`. Provides local neural network inference for semantic search embeddings.

**macOS arm64:**
```sh
curl -fSL "https://github.com/microsoft/onnxruntime/releases/download/v1.24.4/onnxruntime-osx-arm64-1.24.4.tgz" \
  | tar xz -C /tmp
sudo cp /tmp/onnxruntime-osx-arm64-1.24.4/lib/libonnxruntime.1.24.4.dylib /usr/local/lib/
sudo ln -sf libonnxruntime.1.24.4.dylib /usr/local/lib/libonnxruntime.dylib
```

**macOS amd64:**
```sh
curl -fSL "https://github.com/microsoft/onnxruntime/releases/download/v1.24.4/onnxruntime-osx-x86_64-1.24.4.tgz" \
  | tar xz -C /tmp
sudo cp /tmp/onnxruntime-osx-x86_64-1.24.4/lib/libonnxruntime.1.24.4.dylib /usr/local/lib/
sudo ln -sf libonnxruntime.1.24.4.dylib /usr/local/lib/libonnxruntime.dylib
```

**Linux amd64:**
```sh
curl -fSL "https://github.com/microsoft/onnxruntime/releases/download/v1.24.4/onnxruntime-linux-x64-1.24.4.tgz" \
  | tar xz -C /tmp
sudo cp /tmp/onnxruntime-linux-x64-1.24.4/lib/libonnxruntime* /usr/local/lib/
sudo ldconfig
```

### Build & Test

```sh
cd toolshed

# Build everything (compile check)
CGO_ENABLED=1 go build ./...

# Run all tests
CGO_ENABLED=1 go test ./...

# Build the SSH server binary
CGO_ENABLED=1 go build -o toolshed-ssh ./cmd/ssh

# Build the wordcount provider
go build -o wordcount ./cmd/wordcount
```

### Running Locally

1. **Start Dolt** with the registry and ledger databases:

```sh
# Initialize databases (first time only)
mkdir -p /tmp/dolt-data/toolshed_registry /tmp/dolt-data/toolshed_ledger

cd /tmp/dolt-data/toolshed_registry && dolt init && dolt sql < /path/to/toolshed/schema/registry/001_init.sql
cd /tmp/dolt-data/toolshed_ledger && dolt init && dolt sql < /path/to/toolshed/schema/ledger/001_init.sql

# Start the SQL server
dolt sql-server --host 0.0.0.0 --port 3306 --data-dir /tmp/dolt-data
```

2. **Start the SSH server:**

```sh
TOOLSHED_SSH_PORT=2222 \
TOOLSHED_HOST_KEY_PATH=.ssh/toolshed_host_key \
TOOLSHED_REGISTRY_DSN="root@tcp(localhost:3306)/toolshed_registry?parseTime=true" \
TOOLSHED_LEDGER_DSN="root@tcp(localhost:3306)/toolshed_ledger?parseTime=true" \
./toolshed-ssh
```

3. **Connect:**

```sh
ssh -p 2222 localhost help
ssh -p 2222 localhost search "fraud detection"
ssh -p 2222 localhost     # interactive TUI
```

### Embedding Model (optional)

Semantic search requires a local ONNX model. Download it with:

```sh
./scripts/download-model.sh
```

Then set `TOOLSHED_MODEL_DIR=/path/to/model` when starting the SSH server.

## Deployment

The project deploys to Fly.io via Docker. See `deploy/Dockerfile` and `deploy/start.sh`.

```sh
fly deploy
```

The Docker build handles all native dependencies automatically — `libtokenizers`, ONNX Runtime, Dolt, and the embedding model are all fetched during the multi-stage build.

## Project Structure

```
toolshed/
├── cmd/
│   ├── ssh/          # SSH server entrypoint
│   └── wordcount/    # Example tool provider
├── deploy/
│   ├── Dockerfile    # Multi-stage build for Fly.io
│   └── start.sh      # Container entrypoint (Dolt + SSH)
├── internal/
│   ├── core/         # Domain types, YAML parsing, content hashing
│   ├── crawl/        # .well-known/toolshed.yaml crawler
│   ├── dolt/         # Registry & ledger queries
│   ├── embeddings/   # ONNX embedder, cosine similarity
│   └── ssh/          # SSH server, commands, TUI, rate limiting
├── schema/
│   ├── registry/     # Dolt DDL for the shared registry
│   └── ledger/       # Dolt DDL for the local ledger
├── public/           # Static website served on port 8080
└── scripts/          # Dev utilities
```
