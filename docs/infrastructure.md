# Infrastructure Plan

A phased build-out for The Agent Toolshed — from Dolt schema to decentralized registry.

**Core principle: ToolShed is a registry and discovery layer, not a payment processor.** Tool records declare pricing and payment info. ToolShed surfaces it. The agent operator and tool provider settle payment between themselves. ToolShed's job is to make tools findable, callable, and accountable — not to move money.

---

## Repo Structure

```
toolshed/
├── internal/
│   ├── core/             # Go: types, validation, content hashing, YAML marshalling
│   ├── crawl/            # Go: .well-known/toolshed.yaml crawler
│   ├── dolt/             # Go: registry queries, ledger writes
│   ├── embeddings/       # Go: ONNX embedder, cosine similarity, vector encode/decode
│   └── ssh/              # Go: SSH server, TUI, command handlers
├── cmd/
│   ├── ssh/              # Go: SSH server entrypoint (main.go)
│   └── wordcount/        # Go: example tool (REST API)
├── deploy/
│   ├── Dockerfile        # Multi-stage: CGo build + model download + ONNX Runtime
│   └── start.sh          # Entrypoint: Dolt init + SSH server
├── schema/
│   ├── registry/         # Shared Dolt DB — definitions, listings, upvotes, reputation, embeddings
│   │   ├── 001_init.sql
│   │   ├── 002_embeddings.sql
│   │   └── seed.sql
│   └── ledger/           # Local Dolt DB — invocations (private, per-node)
│       └── 001_init.sql
├── scripts/
│   ├── setup-embeddings.sh   # One-shot: downloads libtokenizers + ONNX Runtime + model
│   └── download-model.sh     # Downloads ONNX model from HuggingFace
├── public/               # Static website (toolshed.sh)
├── lib/                  # Native libraries (libtokenizers.a, libonnxruntime.dylib)
├── docker-compose.yml    # Local dev: Dolt SQL server
├── fly.toml              # Fly.io deployment config
├── go.mod
└── go.sum
```

### Why Go

Go is the primary language for the entire server stack. TypeScript is used only for the MCP server and web UI.

- **Dolt is written in Go.** The `dolthub/dolt/go` libraries allow deep integration — programmatic commits, branches, diffs, merges — and the potential to embed the Dolt engine directly for self-hosted deployments. Same language, same ecosystem, same team.
- **Battle-tested MySQL driver.** `go-sql-driver/mysql` is the gold standard. Dolt speaks the MySQL wire protocol.
- **One language for the core.** Gateway and CLI share `internal/` packages directly — no FFI, no WASM, no napi bindings, no type drift. The MCP server is a thin TypeScript HTTP client that talks to the gateway API — it doesn't need Go internals.
- **Single binary, zero runtime.** Cross-compile with `GOOS/GOARCH`, ship a static binary, `FROM scratch` Docker images. No Node runtime to bundle.
- **Content hashing is deterministic by construction.** Go structs with `json.Marshal` preserve field order. The struct _is_ the canonical form — same binary produces the same hash everywhere.
- **Fast compilation.** `go build` takes seconds, not minutes. For a project with gateway + MCP server + CLI, iteration speed compounds over months.

### Shared Types (Source of Truth)

Go structs in `internal/core/` are the single source of truth for all types. JSON Schema is generated from Go structs via `invopop/jsonschema`. The web UI consumes an OpenAPI spec generated from the gateway's routes. Validation uses `go-playground/validator` with struct tags.

```
internal/core/
├── types.go          # ToolDefinition, ToolListing, Upvote, Account, etc.
├── hash.go           # ContentHash() — sha256 of canonical JSON
├── validate.go       # Struct validation rules
└── schema.go         # JSON Schema generation from Go types
```

---

## Phase 0: Foundation — The Monorepo & Dolt Registry

**Goal:** Get the data layer running and the schema real.

- **Dolt DB instance** — stand up a Dolt database with the SQL schema. Host on DoltHub for the public registry. If the schema works and you can `dolt clone` it, the data layer is real.
- **Content hashing utility** — implement `sha256(schema + invocation + provider)` to produce `content_hash`. Nail down the canonical form early: Go structs with deterministic `json.Marshal`, sorted keys via struct field ordering. This is the backbone identity system.
- **Shared types & validation** — Go structs with struct tags for validation (`go-playground/validator`). Generate JSON Schema via `invopop/jsonschema` for external consumption. These become the single source of truth that everything else validates against.

### Two Dolt Databases

The design doc specifies two separate Dolt databases — this split must be real from day one, not refactored in later.

**Registry DB (shared, clonable)**

```
schema/registry/001_init.sql

Tables: accounts, tool_definitions, tool_listings, upvotes, reputation
Hosted on DoltHub: toolshed/registry
Anyone can: dolt clone toolshed/registry
```

This is the public catalog. It contains everything needed for discovery, reputation computation, and identity verification. No secrets, no call data, no private state.

**Ledger DB (local, private, per-node)**

```
schema/ledger/001_init.sql

Tables: invocations
Never shared. Never cloned. Never pushed to DoltHub.
Lives on the gateway's local disk or a private Dolt instance.
```

This is the audit trail. Every tool call gets a Dolt commit with the invocation record (hashes, timing, success/failure). The raw call inputs and outputs are NOT stored — only their hashes. This is what makes "we don't store your data" true while still giving both parties an auditable receipt.

**In code**, the gateway holds two database connections:

```go
type Gateway struct {
    registry *sql.DB   // shared Dolt — tool lookups, account checks
    ledger   *sql.DB   // local Dolt — invocation writes
    // ...
}
```

Migrations are separate. The registry schema evolves via PRs on DoltHub. The ledger schema evolves via the gateway binary (applied on startup).

**Milestone:** Write a tool definition to Dolt, get back a content hash, create a listing that points to it, and query both. All via SQL.

---

## Phase 1: The Gateway — Make a Tool Call Work End-to-End

**Goal:** One tool, one caller, one proxied call, one invocation record.

### Gateway Service

A lightweight, stateless HTTP service built with Go (`net/http` + `chi` for routing). The gateway is a **thin proxy** — it routes requests to providers, validates schemas, and logs invocations. It does not process payments.

On each call it:

1. Looks up the tool listing + definition from Dolt (via `go-sql-driver/mysql`)
2. Validates the caller's input against the tool's declared schema
3. Routes the call to the provider's endpoint based on `invocation.protocol`
4. Validates the provider's response against the tool's output schema
5. Records the invocation in the local Dolt ledger (hashes, timing, success/failure)
6. Returns the result + `invocation_id` + payment info for the tool

The gateway is **Cloudflare, not AWS** — route the request, log the metadata, forget the rest. It never stores call inputs, call outputs, or response bodies.

### Protocol Routing

Start with `"protocol": "rest"` (plain HTTP POST). MCP and gRPC adapters come later. The abstraction: read the `invocation` field, call the right adapter from `internal/protocol/`.

### Gateway API Contract

The gateway exposes four HTTP endpoints. These are the contract between the Go backend and the TypeScript MCP server — and anything else that wants to talk to the gateway.

**`POST /api/search`**

```json
// Request
{
  "capabilities": ["fraud", "financial"],
  "max_price": 0.01,
  "min_reputation": 3.5,
  "limit": 10,
  "offset": 0
}

// Response (200 OK)
{
  "tools": [
    {
      "id": "com.toolshed.tool/fraud-detection@acme.com",
      "name": "Fraud Detection",
      "definition_hash": "sha256:a1b2c3d4e5f6...",
      "description": "Real-time transaction fraud scoring with ML",
      "capabilities": ["fraud", "ml", "financial", "real-time"],
      "pricing": { "model": "per_call", "price": 0.005, "currency": "usd" },
      "payment": {
        "methods": [
          { "type": "stripe", "account_id": "acct_acme_abc123", "billing_url": "https://acme.com/billing" },
          { "type": "api_key", "signup_url": "https://acme.com/developers" },
          { "type": "free" }
        ]
      },
      "reputation": { "avg_quality": 4.3, "verified_upvotes": 847, "sla_compliance_pct": 99.2 },
      "provider": { "domain": "acme.com" }
    }
  ],
  "total": 1,
  "has_more": false
}
```

**`POST /api/invoke`**

```json
// Request
{
  "tool": "fraud-detection@acme.com",
  "input": {
    "transaction_id": "tx_789",
    "amount": 1250.00,
    "merchant_category": "electronics"
  }
}

// Response (200 OK)
{
  "invocation_id": "inv_abc123",
  "definition_hash": "sha256:a1b2c3d4e5f6...",
  "result": {
    "risk_score": 0.85,
    "flags": ["high_amount", "new_merchant"]
  },
  "meta": {
    "latency_ms": 142,
    "schema_valid": true
  },
  "payment_info": {
    "price": 0.005,
    "currency": "usd",
    "methods": [
      { "type": "stripe", "account_id": "acct_acme_abc123" },
      { "type": "api_key", "signup_url": "https://acme.com/developers" }
    ]
  }
}

// Response (502 Bad Gateway)
{
  "error": {
    "code": "provider_error",
    "message": "Provider returned HTTP 500",
    "tool": "fraud-detection@acme.com",
    "latency_ms": 2340
  }
}
```

The `payment_info` block is informational — it tells the agent/operator how to pay the provider for this call. ToolShed surfaces it, doesn't process it.

**`GET /api/reputation/:tool_id`**

```json
// Response (200 OK)
{
  "tool_id": "com.toolshed.tool/fraud-detection@acme.com",
  "total_upvotes": 1203,
  "verified_upvotes": 847,
  "avg_quality": 4.3,
  "sla_compliance_pct": 99.2,
  "schema_compliance_pct": 99.8,
  "unique_callers": 156,
  "total_invocations": 48302,
  "trend": "stable",
  "computed_at": "2026-03-15T14:00:00Z"
}

// Response (404 Not Found)
{
  "error": {
    "code": "tool_not_found",
    "message": "No tool found with id: nonexistent-tool@example.com"
  }
}
```

**`POST /api/review`**

```json
// Request
{
  "tool": "fraud-detection@acme.com",
  "invocation_id": "inv_abc123",
  "quality": 5,
  "useful": true
}

// Response (201 Created)
{
  "upvote_id": "upv_xyz789",
  "proof": {
    "invocation_hash": "sha256:deadbeef...",
    "ledger_commit": "dolt:76qerj11u38il8rb1ddjn3d6kivqamk2",
    "called_at": "2026-03-15T14:23:00Z"
  }
}

// Response (400 Bad Request)
{
  "error": {
    "code": "invalid_invocation",
    "message": "Invocation inv_fake does not exist or does not belong to caller"
  }
}
```

**Standard error envelope** — every error response uses the same shape:

```json
{
  "error": {
    "code": "string",
    "message": "string",
    "field": "string"
  }
}
```

Error codes:

| Code                       | HTTP Status | Meaning                                      |
| -------------------------- | ----------- | -------------------------------------------- |
| `invalid_request`          | 400         | Malformed input, missing fields, bad types   |
| `invalid_schema`           | 400         | Input doesn't match tool's declared schema   |
| `invalid_invocation`       | 400         | Invocation ID doesn't exist or wrong caller  |
| `tool_not_found`           | 404         | Tool ID doesn't resolve to a listing         |
| `rate_limited`             | 429         | Caller exceeded rate limit                   |
| `provider_error`           | 502         | Provider endpoint returned an error          |
| `provider_timeout`         | 504         | Provider didn't respond within SLA           |
| `provider_schema_mismatch` | 502         | Provider response didn't match output schema |
| `internal_error`           | 500         | Something broke in the gateway               |

### Auth & Identity

Auth evolves across phases. Each phase has an explicit trust model:

**Phase 1 — Header trust (development only)**

The MCP server passes `TOOLSHED_ACCOUNT_ID` as an `X-Toolshed-Account` header. The gateway trusts it. No verification, no tokens. This is fine for local development and lets you build the full invoke flow without an auth system.

```
MCP Server → X-Toolshed-Account: acct_test123 → Gateway trusts it
```

**Phase 2 — API keys**

Each account gets a `toolshed_sk_...` secret key, generated on account creation and stored hashed in the `accounts` table. Passed as `Authorization: Bearer toolshed_sk_...`. The gateway hashes and looks up.

```
MCP Server → Authorization: Bearer toolshed_sk_abc... → Gateway verifies hash
```

**Phase 3 — Domain-verified identity**

Domain verification (DNS TXT or `/.well-known/toolshed.json`) ties the account to a real domain. The API key identifies the account; domain verification establishes trust level.

The gateway attaches identity context to every request internally:

```go
type CallerContext struct {
    AccountID       string    // acct_abc123
    Domain          string    // acme.com
    IsProvider      bool
    IsOperator      bool
    DomainVerified  bool
}
```

### Error Handling

**Gateway errors** — the gateway never leaks provider internals. Every provider error is translated into the standard error envelope. The raw provider response is hashed and logged in the Dolt ledger (for audit), but the agent only sees the gateway's error code.

**Failure modes and what happens:**

```
Provider returns 4xx     → Gateway returns provider_error (502)
                           Invocation recorded as success=false in ledger.

Provider returns 5xx     → Gateway returns provider_error (502)
                           Invocation recorded as success=false.

Provider times out       → Gateway returns provider_timeout (504)
                           Timeout = min(tool.sla.p99_latency_ms * 2, 30s)
                           Invocation recorded as success=false.

Provider returns bad     → Gateway returns provider_schema_mismatch (502)
schema                     Invocation recorded with schema_valid=false.
                           Repeated mismatches tank reputation.

Gateway can't reach      → Gateway returns provider_error (502)
Dolt (registry)            Gateway should cache hot tool lookups
                           to survive brief Dolt outages.

Gateway can't write      → Log the failure. Retry async.
to Dolt (ledger)           The invocation still succeeded for the caller.
                           Reconcile later from gateway logs.
```

**Retry policy** — the gateway does NOT retry provider calls. If the provider is flaky, the agent can retry at the MCP level (or pick a different tool). The gateway retries only its own internal operations (Dolt ledger writes).

**Timeouts** — default provider timeout is 30 seconds. If the tool has an SLA with `p99_latency_ms`, the gateway uses `min(p99 * 2, 30s)`. Configurable globally via `TOOLSHED_PROVIDER_TIMEOUT_MS`.

### Search Implementation

Search must work from day one — it's the entry point for the entire system.

**Phase 1 — SQL LIKE queries on Dolt (shipped 2026-03-16)**

Basic substring matching against name, description, and capabilities JSON:

```sql
SELECT DISTINCT tl.*, td.capabilities_json
FROM tool_listings tl
JOIN tool_definitions td ON tl.definition_hash = td.content_hash
WHERE tl.name LIKE ?
   OR tl.description LIKE ?
   OR td.capabilities_json LIKE ?
ORDER BY tl.name
```

This works for single-keyword queries (`"ml"`, `"fraud"`) but fails for multi-word queries like `"fraud detection"` where the exact substring doesn't appear in any field.

**Phase 2 — Semantic search with local ONNX embeddings (shipped 2026-04-06)**

Vector embeddings generated locally via ONNX Runtime — no external API calls.

| Component  | Choice                             | Notes                              |
| ---------- | ---------------------------------- | ---------------------------------- |
| Model      | jinaai/jina-embeddings-v2-small-en | 33M params, 512 dims, Apache 2.0   |
| Runtime    | yalue/onnxruntime_go               | Go bindings for ONNX Runtime (CGo) |
| Tokenizer  | daulet/tokenizers                  | HuggingFace tokenizers via CGo     |
| Storage    | `tool_embeddings` table in Dolt    | Binary-encoded float32 BLOBs       |
| Similarity | Cosine similarity in Go            | Threshold 0.3, top-20 results      |

**How it works:**

1. **On registration/crawl:** tool text is built from `name + description + capabilities + provider domain`, embedded via ONNX, and stored as a binary BLOB in `tool_embeddings`.
2. **On search:** the query is embedded, cosine similarity is computed against all stored vectors in Go, and results are returned ranked by relevance.
3. **Fallback:** if the embedder is not configured or no embeddings exist, search falls back to SQL LIKE.
4. **Backfill:** on startup, any tools missing embeddings are automatically embedded.

**Environment variables:**

```
TOOLSHED_MODEL_DIR    — path to directory containing model.onnx + tokenizer.json
TOOLSHED_ONNX_LIB    — path to ONNX Runtime shared library (auto-detected if not set)
```

**Phase 3 — Weighted ranking (future)**

Combine embedding similarity scores with reputation signals:

```
score = (0.5 * cosine_similarity)
      + (0.25 * normalized_quality)
      + (0.15 * log(verified_upvotes + 1))
      + (0.1 * caller_diversity_score)
```

### Rate Limiting

The gateway rate-limits from day one. You're proxying calls to other people's infrastructure — a runaway agent can't be allowed to hammer a provider through the gateway.

**Per-account, per-tool token bucket:**

```
Default:        60 calls/minute per account per tool
Tool override:  tool.sla.rate_limit (e.g., "1000/min") sets the ceiling
Global:         1000 calls/minute per account across all tools
```

Implementation: in-memory token bucket (`golang.org/x/time/rate`). Single-node constraint for MVP means this just works. The limiter map is keyed by `(account_id, tool_id)`.

HTTP status 429 with `Retry-After` header when limited.

**Milestone:** An HTTP request hits the gateway, the gateway calls a provider's REST endpoint, validates the response, and the invocation appears in the local Dolt ledger.

---

## Phase 2: The MCP Server — Make Agents Talk to It

**Goal:** An agent adds one config line and can discover + invoke tools.

### MCP Server (TypeScript, `@modelcontextprotocol/sdk`)

Exposes the four meta-tools via the official MCP SDK:

- **`toolshed_search`** → `POST /api/search` on the gateway
- **`toolshed_invoke`** → `POST /api/invoke` on the gateway
- **`toolshed_reputation`** → `GET /api/reputation` on the gateway
- **`toolshed_review`** → `POST /api/review` on the gateway

This is a thin HTTP client — it translates MCP tool calls into gateway API calls. It doesn't touch Dolt directly. All state lives in the gateway and Dolt. The gateway's HTTP API is the contract; the MCP server is just a membrane between the MCP protocol and that API.

### Why TypeScript for the MCP Server

The MCP server is a leaf node, not core infrastructure. What matters is:

- **Protocol correctness** — `@modelcontextprotocol/sdk` is the reference implementation
- **Zero-friction distribution** — `npx @agent-toolshed/mcp-server` works out of the box in Claude Code, Cursor, Windsurf, and every agent runtime that supports MCP
- **Minimal surface area** — ~200 lines of code, four tool handlers, each making one HTTP call to the gateway

**Milestone:** Add the MCP server to Claude Code or Cursor, ask the agent to "find a fraud detection tool and call it," and it works. This is the demo moment.

---

## Phase 3: The Web UI & CLI — Make Humans Productive

**Goal:** Providers can list tools, operators can browse, everyone can search.

### `toolshed.sh` Web App

A registry browser (think npmjs.com), built with Next.js or Astro:

- Browse and search tools, view schemas, see reputation
- Provider dashboard: list a tool, manage listings, view invocation stats
- Operator view: browse tools, see payment info, compare options
- TypeScript client generated from the gateway's OpenAPI spec

### CLI

Built with Go + `cobra`. Single static binary, cross-compiles for linux/mac/windows:

- `toolshed publish` — register a tool from a JSON file
- `toolshed search` — find tools by capability
- `toolshed verify-domain` — prove domain ownership
- `toolshed whoami` — show current account info

Distributed via GitHub Releases, Homebrew, and `go install`.

### Domain Verification

Implement DNS TXT record and `/.well-known/toolshed.json` checks. This is the identity layer — verified domains carry more weight in search and reputation.

**Milestone:** A provider signs up at `toolshed.sh`, verifies their domain, pastes a tool definition JSON, and sees it appear in the public registry.

---

## Phase 4: Reputation & Discovery — Make the Flywheel Spin

**Goal:** Upvotes accumulate, reputation materializes, search gets smart.

### Reputation Without Payment Proof

Since ToolShed doesn't process payments, proof-of-use is based on invocation records in the Dolt ledger rather than payment receipts. An upvote is credible if:

1. The `invocation_id` exists in the caller's ledger
2. The invocation's `tool_id` matches the upvote's `subject`
3. The `called_at` timestamp is recent and plausible
4. The caller is a distinct account from the provider

This is weaker than payment proof (you can't verify money changed hands), but it's strong enough to bootstrap. Invocation logging through the gateway is the proof layer — you can't upvote a tool you didn't call through the system.

**Anti-gaming properties in a no-payment model:**

| Attack                   | Why It's Still Hard                                             |
| ------------------------ | --------------------------------------------------------------- |
| **Fake upvotes** (sybil) | Must have a valid invocation ID from the gateway's ledger       |
| **Self-upvoting**        | Ledger shows `caller == provider` — trivial to filter           |
| **Wash trading**         | Detectable via diversity-of-callers weighting                   |
| **Deleting bad reviews** | Upvotes live in the reviewer's record space, not the provider's |

The main weakness: without payment proof, someone could spin up accounts and make free tool calls to inflate reputation. Mitigation: weight upvotes by caller age, domain verification status, and diversity. Rate limiting on the gateway also caps how fast anyone can generate invocations.

- **Upvote ingestion** — validate the invocation ID exists, write to the shared Dolt registry
- **Reputation materialization** — a scheduled job or Dolt stored procedure that aggregates upvotes into the `reputation` table, weighted by caller diversity, verification status, and recency
- **Search ranking** — factor in reputation score, invocation volume, SLA compliance, and price

**Milestone:** Two tools serve the same capability. The one with better verified upvotes ranks higher in `toolshed_search`. The flywheel is turning.

---

## Phase 5: Federation & Self-Hosting — The Exit Ramp

**Goal:** Anyone can run their own Toolshed.

- **`dolt clone toolshed/registry`** — already works if the public registry is on DoltHub. Schema, seed data, and materialization queries must be self-contained.
- **Self-hosted gateway** — a single Docker image (or static binary) with the gateway and optionally an embedded Dolt engine. Internal tools never touch the public registry. `docker compose up` with the provided compose file gets you running in minutes.
- **Registry sync** — `dolt pull` from the public registry to get new tools. `dolt push` to contribute back (with review/PR process on DoltHub).

**Milestone:** A company runs their own Toolshed instance with a mix of public tools (synced from DoltHub) and private internal tools.

---

## Phase 6: Decentralized Registry — The AT Protocol Evolution

**Goal:** Providers self-host their tool records. ToolShed becomes an indexer, not the source of truth.

This is the Version C evolution. Instead of publishing to a ToolShed-hosted Dolt registry, providers publish tool records on their own domains:

```
Provider's domain (acme.com)
  └── /.well-known/toolshed.json
      {
        "tools": [
          {
            "definition": { "schema": {...}, "invocation": {...}, ... },
            "listing": { "name": "Fraud Detection", "pricing": {...}, "payment": {...} }
          }
        ]
      }
```

ToolShed crawls these endpoints and indexes the records into Dolt for search. The data lives on the provider's domain — if ToolShed disappears, the tool records still exist. This is the AT Protocol philosophy: **data outlives the platform.**

The transition is smooth:

1. Today (Phase 0-4): providers publish directly to the Dolt registry
2. Tomorrow (Phase 6): providers host records on their domain AND the registry indexes them
3. Eventually: the registry is purely an index — the provider's domain is the source of truth

The MCP server interface doesn't change. The agent doesn't know or care where the registry data came from. Discovery works the same way.

**What this requires:**

- A crawler that fetches `/.well-known/toolshed.json` from known domains
- A reconciliation process: if the provider's hosted record differs from the registry, the provider's version wins
- A discovery mechanism for new providers (submit your domain, or be discovered via upvotes/links)
- Content hash verification: the crawler re-computes the hash and rejects records where the hash doesn't match the content

**Milestone:** A provider hosts their tool record on their own domain. ToolShed discovers it, indexes it, and agents find it via search — without the provider ever touching the Dolt registry directly.

---

## Cross-Cutting Concerns

### Testing Strategy

Each layer gets a different testing approach:

**Unit tests — `internal/core/`**

Pure functions, no external dependencies. These run fast and run everywhere.

- Content hashing: same input → same hash, always. Different inputs → different hashes. Test with 1000 iterations for determinism.
- Validation: struct tags reject bad data. Test every field constraint.
- Schema matching: input/output validation against tool schemas.
- Search scoring: given mock reputation data, verify ranking order.

```
go test ./internal/core/... -count=1
```

**Integration tests — `internal/dolt/`**

Require a running Dolt instance. Use a dedicated test database that gets reset between runs.

- Write a tool definition → read it back → verify content hash matches.
- Write a listing → search by capability → verify it's returned.
- Write an invocation → query the ledger → verify the record.
- Time-travel: write, commit, write again, query `AS OF` the first commit.

```
# Start a test Dolt instance
dolt sql-server --port 3307 &
TOOLSHED_TEST_DOLT_DSN="root@tcp(127.0.0.1:3307)/test_registry" go test ./internal/dolt/...
```

**Gateway end-to-end tests — `cmd/gateway/`**

A test harness that:

1. Starts the gateway against a test Dolt instance
2. Registers a mock tool provider (a simple HTTP server in the test)
3. Inserts a tool definition + listing into the test registry
4. Calls `POST /api/invoke` and verifies the full flow: lookup → route → validate → ledger → response

```go
func TestInvokeEndToEnd(t *testing.T) {
    // Start mock provider
    provider := httptest.NewServer(mockFraudHandler())
    defer provider.Close()

    // Insert tool pointing to mock provider
    seedTool(t, testRegistry, provider.URL)

    // Call the gateway
    resp := post(t, gateway.URL+"/api/invoke", invokeRequest{
        Tool:  "test-fraud@example.com",
        Input: map[string]any{"transaction_id": "tx_1", "amount": 100},
    })

    assert.Equal(t, 200, resp.StatusCode)
    assert.NotEmpty(t, resp.Body.InvocationID)
    assert.True(t, resp.Body.Meta.SchemaValid)
}
```

**MCP server tests — `apps/mcp-server/`**

The MCP server is a thin HTTP client. Test it against a mock gateway (not a real one).

- Mock `POST /api/search` → verify `toolshed_search` returns formatted results.
- Mock `POST /api/invoke` → verify `toolshed_invoke` passes input through correctly.
- Mock error responses → verify the MCP server translates them into MCP error format.

```
cd apps/mcp-server && npm test
```

**What NOT to test:**

- Don't test Dolt's SQL engine. Test that your queries return what you expect.
- Don't test the MCP SDK. Test that your handlers do the right thing.

### Observability

The gateway proxies calls to third-party infrastructure. You need to know what's happening without storing call data.

**Structured logging** — every gateway request gets a structured log line with:

```json
{
  "level": "info",
  "msg": "invoke_complete",
  "account_id": "acct_abc123",
  "tool_id": "fraud-detection@acme.com",
  "definition_hash": "sha256:a1b2c3...",
  "invocation_id": "inv_xyz789",
  "latency_ms": 142,
  "schema_valid": true,
  "success": true,
  "ts": "2026-03-15T14:23:00Z"
}
```

What gets logged: account IDs, tool IDs, hashes, timing, success/failure.
What does NOT get logged: call inputs, call outputs, provider response bodies. These never touch disk.

Use `log/slog` (stdlib since Go 1.21). JSON output. No external logging framework needed for MVP.

**Metrics** — expose Prometheus-compatible metrics from the gateway:

```
toolshed_invoke_total{tool, status}                        — counter
toolshed_invoke_latency_ms{tool}                           — histogram
toolshed_search_total{status}                              — counter
toolshed_provider_timeout_total{tool}                      — counter
toolshed_rate_limit_total{account, tool}                   — counter
toolshed_schema_mismatch_total{tool}                       — counter
```

For MVP, expose at `GET /metrics`. No Grafana, no Datadog — just the endpoint. Dashboards come when there's traffic to look at.

### Configuration

The gateway is configured via environment variables with sensible defaults. No config files for MVP — env vars are the simplest thing that works with Docker, Fly.io, and local development.

```
# Required
TOOLSHED_REGISTRY_DSN=root@tcp(127.0.0.1:3306)/toolshed_registry
TOOLSHED_LEDGER_DSN=root@tcp(127.0.0.1:3306)/toolshed_ledger

# Optional (with defaults)
TOOLSHED_PORT=8080
TOOLSHED_LOG_LEVEL=info                    # debug, info, warn, error
TOOLSHED_LOG_FORMAT=json                   # json, text
TOOLSHED_PROVIDER_TIMEOUT_MS=30000         # default provider call timeout
TOOLSHED_RATE_LIMIT_DEFAULT=60             # calls/min per account per tool
TOOLSHED_RATE_LIMIT_GLOBAL=1000            # calls/min per account across all tools
TOOLSHED_AUTH_MODE=header                  # header (Phase 1), apikey (Phase 2)

# Semantic search (optional — omit to use LIKE-only search)
TOOLSHED_MODEL_DIR=/model                 # ONNX model directory (model.onnx + tokenizer.json)
TOOLSHED_ONNX_LIB=/usr/local/lib/libonnxruntime.so  # ONNX Runtime shared library path
```

Parsed at startup into a typed `Config` struct. The gateway fails fast on missing required vars — no silent defaults for database connections.

```go
type Config struct {
    RegistryDSN      string        `env:"TOOLSHED_REGISTRY_DSN,required"`
    LedgerDSN        string        `env:"TOOLSHED_LEDGER_DSN,required"`
    Port             int           `env:"TOOLSHED_PORT" envDefault:"8080"`
    LogLevel         string        `env:"TOOLSHED_LOG_LEVEL" envDefault:"info"`
    ProviderTimeout  time.Duration `env:"TOOLSHED_PROVIDER_TIMEOUT_MS" envDefault:"30000"`
    RateLimitDefault int           `env:"TOOLSHED_RATE_LIMIT_DEFAULT" envDefault:"60"`
    RateLimitGlobal  int           `env:"TOOLSHED_RATE_LIMIT_GLOBAL" envDefault:"1000"`
    AuthMode         string        `env:"TOOLSHED_AUTH_MODE" envDefault:"header"`
}
```

---

## What to Build First

Start with **Phase 0 + the happy path of Phase 1** in parallel:

1. Get the Dolt schema live and write a few example tool definitions by hand
2. Build the content-hashing function and the shared Go types in `internal/core`
3. Stand up a minimal gateway (`cmd/gateway`) that can do one end-to-end proxied call

This gives a working vertical slice — data layer, routing, and invocation logging — without any UI, MCP, or reputation complexity. Everything else layers on top.

---

## Deployment Strategy

The Go binary is platform-agnostic. It runs anywhere Docker runs.

### Development & MVP (Phases 0–2)

Go binary on Fly.io, Railway, or a VPS. Dolt hosted on DoltHub or a managed instance. Simple, cheap, single region. Focus on getting the vertical slice working.

### Hosted Service — `toolshed.sh` (Phase 3+)

Move to Cloudflare Containers or a multi-region setup when traffic demands it. A lightweight edge layer (Cloudflare Worker or similar) can sit in front for rate limiting, auth, and DDoS protection.

### Self-Hosted (Phase 5)

```
docker compose up
```

The provided compose file runs the gateway + Dolt. No external dependencies. The same binary, the same image — just a different environment.

### Summary

```
Phase 0–2:  Go binary on Fly.io / Railway / VPS
Phase 3+:   Cloudflare Containers for toolshed.sh (when needed)
Phase 5:    docker compose up (self-hosted, your infra, your rules)
```

---

## Future: Payment Integration

Payment is deliberately deferred. The architecture supports adding it later without redesigning:

**When payment makes sense:**

- The registry has real tools and real agents using them
- Providers want ToolShed to handle billing (not just surface payment info)
- Volume justifies the Stripe Connect overhead

**What it would look like:**

- ToolShed becomes a Stripe Connect platform
- The gateway adds credit drawdown before proxying calls
- Meter events fire async on successful calls
- The `payment_info` block in invoke responses becomes transactional, not just informational
- Tool records gain a `"type": "toolshed_managed"` payment method

**What stays the same:**

- Tool records still declare payment methods (they already do)
- The gateway still routes calls the same way
- The Dolt ledger still logs invocations
- Providers who don't want managed payment keep using `"type": "stripe"` or `"type": "api_key"` or `"type": "free"`

The payment field on the tool record is the extension point. Today it's informational. Tomorrow it can be transactional. The agent-facing interface doesn't change.

---

## Tech Stack

| Component                | Choice                                               | Why                                                                     |
| ------------------------ | ---------------------------------------------------- | ----------------------------------------------------------------------- |
| SSH Server               | Go (`charmbracelet/wish` + `charmbracelet/ssh`)      | SSH-native interface; public key = identity; zero signup                |
| Interactive TUI          | Go (`charmbracelet/bubbletea`)                       | Rich terminal UI over SSH for browsing the registry                     |
| Embeddings (model)       | Jina v2 small (`jinaai/jina-embeddings-v2-small-en`) | 33M params, 512 dims, Apache 2.0 license, ONNX available                |
| Embeddings (runtime)     | ONNX Runtime (`yalue/onnxruntime_go`)                | Local inference, no external API calls, CGo bindings                    |
| Embeddings (tokenizer)   | HuggingFace Tokenizers (`daulet/tokenizers`)         | Accurate BPE tokenization via CGo Rust bindings                         |
| Registry DB              | Dolt (MySQL-compatible, Git-like versioning)         | Content-addressed tools; `dolt clone` for federation                    |
| Web UI                   | Static HTML/CSS                                      | Lightweight; served by Go `http.FileServer`                             |
| Domain Crawling          | Go (`net/http`)                                      | Fetches `/.well-known/toolshed.yaml` from provider domains              |
| Shared types             | Go structs → YAML (`gopkg.in/yaml.v3`)               | Single source of truth; deterministic serialization for content hashing |
| Deployment (hosted)      | Fly.io (single container: Dolt + SSH + HTTP)         | Persistent volume for Dolt; dedicated IPv4 for SSH on port 22           |
| Deployment (self-hosted) | Docker / `docker compose`                            | Same image as hosted; no platform dependencies                          |
