# ToolShed — Build Plan

**From design doc to working system. Concrete steps, in order, with nothing hand-waved.**

## Progress

| Step   | Name                   | Status         | Completed  |
| ------ | ---------------------- | -------------- | ---------- |
| **1**  | Repo Structure         | ✅ Done        | 2026-03-16 |
| **2**  | Core Types             | ✅ Done        | 2026-03-16 |
| **3**  | Content Hashing        | ✅ Done        | 2026-03-16 |
| **4**  | Dolt Schema            | ✅ Done        | 2026-03-16 |
| **5**  | SSH Server & Commands  | ✅ Done        | 2026-03-16 |
| **6**  | Wire Up a Real Tool    | ✅ Done        | 2026-03-16 |
| **7**  | Interactive TUI        | ✅ Done        | 2026-03-22 |
| **8**  | Domain Crawling        | ✅ Done        | 2026-03-22 |
| **9**  | Semantic Search (ONNX) | ✅ Done        | 2026-04-06 |
| **10** | Fly.io Deployment      | ✅ Done        | 2026-04-06 |
| **11** | MCP Server             | 🔲 Not started | —          |

**Milestones reached:** M1 ✅ "It compiles" · M2 ✅ "It stores" · M3 ✅ "It calls" · M4 ✅ "It discovers" · M5 ✅ "It understands"

> **Architecture pivot (2026-03-16):** Replaced the HTTP gateway + API key auth model with an SSH-native interface. Identity is your SSH public key — zero signup, zero tokens. Commands return YAML. The gateway was replaced by `ssh toolshed.sh <command>`.
>
> **Semantic search (2026-04-06):** Added local ONNX embedding support using Jina v2 (Apache 2.0). Tool names, descriptions, and capabilities are embedded at registration time. Search uses cosine similarity — `ssh toolshed.sh search "fraud detection"` now finds tools by meaning, not just substring matching. Backfill runs automatically at startup for seeded data.

---

## The Map

```
 WEEK 1-2              WEEK 2-3              WEEK 3-4
┌──────────┐       ┌──────────────┐      ┌──────────────┐
│  STEP 1  │       │    STEP 5    │      │    STEP 7    │
│  Repo    │       │  Thin Gateway│      │  MCP Server  │
│  Setup   │       │  (proxy only)│      │  (4 tools)   │
└────┬─────┘       └──────┬───────┘      └──────┬───────┘
     │                    │                     │
┌────▼─────┐       ┌──────▼───────┐      ┌──────▼───────┐
│  STEP 2  │       │    STEP 6    │      │   DEMO DAY   │
│  Core    │       │  Wire Up a   │      │              │
│  Types   │       │  Real Tool   │      │  Agent adds  │
└────┬─────┘       └──────────────┘      │  one config  │
     │                                   │  line, finds │
┌────▼─────┐                             │  & calls a   │
│  STEP 3  │                             │  tool.       │
│  Content │                             └──────────────┘
│  Hashing │
└────┬─────┘
     │
┌────▼─────┐
│  STEP 4  │
│  Dolt    │
│  Schema  │
└──────────┘
```

**No payment. No Stripe. No billing complexity.** The entire focus is: can an agent discover and call a tool it didn't know about?

---

## What Each Step Produces

| Step  | What You Build                               | Input                    | Output                                   | Testable?                        |
| ----- | -------------------------------------------- | ------------------------ | ---------------------------------------- | -------------------------------- |
| **1** | Repo structure, `go.mod`, stub packages      | Infra doc                | `go build ./...` passes                  | ✅ Compiles                      |
| **2** | `internal/core/types.go` — all domain types  | Design doc JSON examples | Go structs with JSON + validation tags   | ✅ Compiles, JSON round-trips    |
| **3** | `internal/core/hash.go` — content addressing | `ToolDefinition` struct  | `ContentHash(def) → "sha256:a1b2c3..."`  | ✅ Unit tests, deterministic     |
| **4** | Dolt DB with SQL schema, seed data           | Registry SQL schema      | `SELECT * FROM tool_listings` works      | ✅ Query returns rows            |
| **5** | `cmd/gateway` — proxy + invocation logging   | Tool lookup from Dolt    | Proxied response from provider           | ✅ `curl` end-to-end             |
| **6** | A real tool running on a real endpoint       | Any simple API           | Registered in Dolt, callable via gateway | ✅ Gateway → tool → response     |
| **7** | `apps/mcp-server` — 4 meta-tools             | Gateway HTTP API         | Agent can search + invoke                | ✅ Works in Claude Code / Cursor |

---

## Dependency Graph

```
                    ┌───────────────────────────────┐
                    │  You can't build this...       │
                    │  ...without this being done.   │
                    └───────────────────────────────┘

Step 1 ─── Repo & go.mod
  │
  ▼
Step 2 ─── Core Types ◄──────────────────────────────── Design doc JSON examples
  │                                                      are your spec
  ├──────────────────┐
  ▼                  ▼
Step 3               Step 4
Content Hashing      Dolt Schema
  │                  │
  │    ┌─────────────┘
  │    │
  ▼    ▼
Step 5 ─── Gateway ◄─────────────────────────────────── Needs types, Dolt, hashing
  │
  ▼
Step 6 ─── Real Tool Provider ◄──────────────────────── Needs gateway to call through
  │
  ▼
Step 7 ─── MCP Server ◄─────────────────────────────── Needs gateway HTTP API to exist
```

**Key insight:** Steps 3 and 4 are independent of each other. You can work on content hashing and Dolt schema in parallel. Everything else is sequential.

---

## Step 1 — Repo Structure

**Time: 1 hour. No excuses.**

```
toolshed/
├── go.mod
├── go.sum
├── internal/
│   ├── core/             # Types, hashing, validation
│   │   ├── types.go
│   │   ├── hash.go
│   │   ├── hash_test.go
│   │   └── validate.go
│   ├── dolt/             # Registry queries, ledger writes
│   │   └── registry.go
│   ├── gateway/          # Request routing, schema validation
│   │   └── handler.go
│   └── protocol/         # Protocol adapters
│       └── rest.go
├── cmd/
│   └── gateway/
│       └── main.go       # Entrypoint
├── apps/
│   └── mcp-server/       # TypeScript MCP server (Step 7)
│       ├── package.json
│       └── src/
│           └── index.ts
├── schema/
│   ├── registry/
│   │   ├── 001_init.sql
│   │   └── seed.sql
│   └── ledger/
│       └── 001_init.sql
└── docs/
    ├── the_idea.md
    ├── infrastructure.md
    └── build_plan.md
```

**Done when:** `go build ./...` passes with stub files.

> **✅ Completed 2026-03-16.** 13 files created. `go build ./...` passes. Structure matches plan exactly (skipped `apps/mcp-server/` and `docs/` — MCP is Step 7, docs already live in parent `toolbox/` dir).

---

## Step 2 — Core Types

**Time: 2-3 hours.**

The structs that drive everything. Pulled directly from the design doc's JSON examples.

```
internal/core/types.go

  Account
  ├── ID              string    // acct_abc123
  ├── Domain          string    // acme.com
  ├── DID             string    // did:plc:acme-corp (optional)
  ├── DisplayName     string
  ├── IsProvider      bool
  ├── IsOperator      bool
  └── CreatedAt       time.Time

  ToolDefinition (immutable, content-addressed)
  ├── ContentHash     string    // sha256:a1b2c3... (computed, not set by caller)
  ├── Provider        Provider
  │   ├── Domain      string
  │   └── DID         string
  ├── Schema          Schema
  │   ├── Input       map[string]FieldDef
  │   └── Output      map[string]FieldDef
  ├── Invocation      Invocation
  │   ├── Protocol    string    // "mcp", "rest", "grpc"
  │   ├── Endpoint    string
  │   └── ToolName    string
  ├── Capabilities    []string
  └── CreatedAt       time.Time

  ToolListing (mutable, points to definition)
  ├── ID              string    // com.toolshed.tool/fraud-detection@acme.com
  ├── DefinitionHash  string    // → ToolDefinition.ContentHash
  ├── Name            string
  ├── VersionLabel    string
  ├── Description     string
  ├── Pricing         Pricing
  │   ├── Model       string    // "per_call", "free", "subscription"
  │   ├── Price       float64
  │   ├── Currency    string
  │   └── Bulk        map[int]float64
  ├── Payment         Payment
  │   └── Methods     []PaymentMethod
  │       ├── Type        string   // "stripe", "api_key", "free"
  │       ├── AccountID   string   // provider's Stripe account (informational)
  │       ├── BillingURL  string   // where to set up billing
  │       └── SignupURL   string   // where to get an API key
  ├── SLA             SLA
  │   ├── P99Latency  int
  │   ├── Uptime      string
  │   └── RateLimit   string
  └── UpdatedAt       time.Time

  Upvote
  ├── ID              string
  ├── ToolID          string    // → ToolListing.ID
  ├── CallerAccount   string
  ├── Proof           InvocationProof
  │   ├── InvocationHash  string
  │   ├── LedgerCommit    string
  │   └── CalledAt        time.Time
  ├── Evaluation      Evaluation
  │   ├── Quality         int      // 1-5
  │   ├── LatencyMetSLA   bool
  │   ├── SchemaValid     bool
  │   └── Useful          bool
  ├── Context         CallContext
  │   ├── AgentRuntime    string
  │   ├── TaskType        string
  │   └── InputComplexity string
  └── CreatedAt       time.Time

  Invocation (local ledger only)
  ├── ID              string
  ├── ToolID          string
  ├── DefinitionHash  string
  ├── CallerAccount   string
  ├── InputHash       string
  ├── OutputHash      string
  ├── LatencyMs       int
  ├── SchemaValid     bool
  ├── Success         bool
  └── CreatedAt       time.Time
```

Note: `PaymentMethod` fields are **informational** — they tell the agent/operator how to pay the provider directly. ToolShed doesn't process these. The `Invocation` struct has no payment fields — we're not tracking money movement.

**Done when:** Types compile, JSON marshal/unmarshal round-trips pass, validation tags reject bad data.

> **✅ Completed 2026-03-16.** All 12 types implemented in `internal/core/types.go`. 7 tests pass: `TestAccountJSON`, `TestToolDefinitionJSON`, `TestToolListingJSON`, `TestUpvoteJSON`, `TestInvocationRecordJSON`, `TestToolDefinitionFromJSON`, plus a placeholder hash test. One deviation from plan: `Pricing.Bulk` is `map[string]float64` (not `map[int]float64`) because JSON object keys are always strings and Dolt returns them that way.

---

## Step 3 — Content Hashing

**Time: 2-3 hours.**

```
ContentHash(ToolDefinition) → "sha256:a1b2c3d4e5f6..."

  What gets hashed (the immutable contract):
  ┌─────────────────────────────────────┐
  │  provider.domain                     │
  │  provider.did                        │
  │  schema.input  (all field defs)      │
  │  schema.output (all field defs)      │──── canonical JSON ──── sha256 ──── "sha256:..."
  │  invocation.protocol                 │
  │  invocation.endpoint                 │
  │  invocation.tool_name                │
  │  capabilities                        │
  └─────────────────────────────────────┘

  What does NOT get hashed (mutable metadata):
  ┌─────────────────────────────────────┐
  │  name                                │
  │  version_label                       │
  │  description                         │
  │  pricing                             │
  │  payment                             │
  │  sla                                 │
  └─────────────────────────────────────┘
```

**Critical property:** Same input → same hash, every time, on every machine. Go structs with `json.Marshal` give you deterministic field ordering because struct fields are serialized in declaration order. Write a test that hashes the same definition 1000 times and asserts they're all equal.

**Done when:** Unit tests pass. Two definitions with identical contracts produce the same hash. Two definitions with different schemas produce different hashes.

> **✅ Completed 2026-03-16.** `ContentHash()` implemented in `internal/core/hash.go`. Uses a separate `hashableDefinition` struct containing only immutable fields (excludes `ContentHash`, `CreatedAt`, and all listing metadata). Capabilities are sorted before hashing. 13 tests pass including: determinism (1000 iterations), identical defs, differing schemas/providers/invocations/capabilities/protocols, exclusion of `ContentHash` and `CreatedAt` fields, map key order independence, and minimal definitions.

---

## Step 4 — Dolt Schema

**Time: 2-3 hours.**

Take the SQL schema, put it in `schema/registry/001_init.sql` and `schema/ledger/001_init.sql`, and get them running:

```
# Registry (shared)
$ dolt init toolshed-registry
$ dolt sql < schema/registry/001_init.sql
$ dolt add .
$ dolt commit -m "Initial schema"
$ dolt sql < schema/registry/seed.sql
$ dolt commit -m "Seed data: example tool definitions"

# Ledger (local)
$ dolt init toolshed-ledger
$ dolt sql < schema/ledger/001_init.sql
$ dolt add .
$ dolt commit -m "Initial schema"

# Verify
$ dolt sql -q "SELECT * FROM tool_definitions"
$ dolt sql -q "SELECT * FROM tool_listings WHERE capabilities_json LIKE '%fraud%'"
$ dolt sql -q "SELECT * FROM tool_listings AS OF 'HEAD~1'"  # time travel works
```

Note: the `invocations` table has no payment columns (no `payment_method`, `payment_amount`, `payment_currency`, `meter_event_id`). It's just: who called what, when, and did it work.

**Done when:** Both schemas are live, seed data is queryable, `dolt log` shows commits, time-travel queries work.

> **✅ Completed 2026-03-16.** Both schemas written. Dolt runs via `docker-compose.yml` (dolthub/dolt-sql-server). Registry has 5 tables (accounts, tool_definitions, tool_listings, upvotes, reputation) + indexes. Ledger has 1 table (invocations) with no payment columns. Seed data includes 3 accounts, 1 fraud-detection definition+listing, 1 upvote, and 1 reputation snapshot. `dolt log` shows 3 registry commits. Time-travel verified: `SELECT COUNT(*) FROM tool_listings AS OF 'HEAD~1'` → 0. One gotcha fixed: seed SQL uses raw JSON strings instead of `JSON_OBJECT()` because Dolt's `JSON_OBJECT` coerces numeric values to strings.

---

## Step 5 — Thin Gateway

**Time: 3-5 days.**

The minimum viable gateway. A proxy that routes, validates, and logs. No payment.

```
            ┌──────────────────────────────────────────────────┐
            │  POST /api/invoke                                 │
            │                                                   │
            │  Request:                                         │
            │  {                                                │
            │    "tool": "fraud-detection@acme.com",            │
            │    "input": { "transaction_id": "tx_123", ... }   │
            │  }                                                │
            │                                                   │
  curl ────►│  Gateway does:                                    │
            │  1. SELECT from tool_listings + tool_definitions  │
            │  2. Validate input against schema                 │
            │  3. Route to provider endpoint (REST adapter)     │
            │  4. Validate output against schema                │
            │  5. Write invocation to local Dolt ledger         │
            │  6. Return result + invocation_id + payment_info  │
            │                                                   │
            │  Response:                                        │
            │  {                                                │
            │    "invocation_id": "inv_abc123",                 │
            │    "result": { "risk_score": 0.85, ... },         │
            │    "meta": { "latency_ms": 142, "schema_valid":   │
            │      true },                                      │
            │    "payment_info": {                               │
            │      "price": 0.005,                              │
            │      "currency": "usd",                           │
            │      "methods": [                                 │
            │        { "type": "stripe",                        │
            │          "account_id": "acct_acme_abc123" }       │
            │      ]                                            │
            │    }                                              │
            │  }                                                │
            └──────────────────────────────────────────────────┘

  Also add:
  ┌──────────────────────────────────────────────────────────┐
  │  POST /api/search   — query tool_listings by capability  │
  │  GET  /api/reputation/:tool_id — query reputation table  │
  │  POST /api/review   — write an upvote record             │
  └──────────────────────────────────────────────────────────┘
```

**payment_info is informational** — it tells the caller how to pay the provider directly. The gateway surfaces it from the tool record but doesn't process anything.

**Hardcode for now:**

- Caller identity (no auth — pretend every request is from `acct_test123`)
- Protocol (REST only — `internal/protocol/rest.go` does `http.Post`)

**Done when:** `curl -X POST localhost:8080/api/invoke -d '{"tool":"...","input":{...}}'` returns a real response from a real provider endpoint, and the invocation is recorded in the Dolt ledger.

> **✅ Completed 2026-03-16.** Full gateway implemented across 5 files:
>
> - **`internal/dolt/registry.go`** — `Registry` struct with 8 methods: `NewRegistry`, `Close`, `GetToolListing`, `GetToolDefinition`, `SearchTools`, `GetReputation`, `WriteInvocation`, `WriteUpvote`. Uses `github.com/go-sql-driver/mysql` over Dolt's MySQL wire protocol.
> - **`internal/core/validate.go`** — `ValidateInput`/`ValidateOutput` with type checking (string, number, boolean, array, object), min/max range validation, recursive array item validation. 18 tests pass.
> - **`internal/protocol/rest.go`** — `Adapter` interface + `RESTAdapter` + `CallProvider`. POSTs JSON to provider, measures latency, 30s default timeout. `NewAdapter("rest")` factory; MCP/gRPC return "not yet supported".
> - **`internal/gateway/handler.go`** — 4 endpoints: `POST /api/invoke` (lookup → validate → call → log → respond), `POST /api/search`, `GET /api/reputation/{toolID}`, `POST /api/review`. Plus logging middleware, CORS middleware, health check.
> - **`cmd/gateway/main.go`** — Config via env vars (`TOOLSHED_PORT`, `TOOLSHED_REGISTRY_DSN`, `TOOLSHED_LEDGER_DSN`), graceful shutdown.
>
> All endpoints verified via curl. `/api/invoke` works end-to-end for the pipeline (lookup → validate → route) but returns "protocol mcp not yet supported" for the seeded fraud-detection tool because it uses MCP. Needs a REST tool registered — that's Step 6. Docker Compose updated with `root@'%'` user creation for external connections.

---

## Step 6 — Wire Up a Real Tool

**Time: 1-2 days.**

Build something trivial. The tool itself doesn't matter — what matters is that it's registered in Dolt and callable through the gateway.

```
  Ideas (pick one, keep it dead simple):
  ┌────────────────────────────────────────────────────────┐
  │  • Sentiment analyzer  — POST text, get score          │
  │  • Currency converter  — POST amount + pair, get rate  │
  │  • Hash generator      — POST string, get sha256       │
  │  • Word counter        — POST text, get stats          │
  └────────────────────────────────────────────────────────┘

  Deploy it anywhere:
  ┌────────────────────────────────────────────────────────┐
  │  • Fly.io (free tier)                                   │
  │  • Cloudflare Worker                                    │
  │  • Railway                                              │
  │  • Literally localhost for now                          │
  └────────────────────────────────────────────────────────┘

  Register it:
  INSERT INTO tool_definitions (content_hash, ...) VALUES (...);
  INSERT INTO tool_listings (id, definition_hash, ...) VALUES (...);

  Call it through the gateway:
  curl -X POST localhost:8080/api/invoke \
    -d '{"tool":"sentiment@yourdomain.com","input":{"text":"this is great"}}'

  Get back:
  {
    "invocation_id": "inv_001",
    "result": { "score": 0.92, "label": "positive" },
    "meta": { "latency_ms": 87, "schema_valid": true },
    "payment_info": { "price": 0, "methods": [{ "type": "free" }] }
  }
```

**Done when:** A tool you built is discoverable via `/api/search` and callable via `/api/invoke` through the gateway. The full loop works.

> **✅ Completed 2026-03-16.** Word Count tool built as `cmd/wordcount/main.go` — a simple REST provider that counts words, characters, sentences, and paragraphs. 21 tests pass (14 unit tests for `countText`, 7 HTTP handler integration tests including error cases and Unicode). Content hash computed via `core.ContentHash()` → `sha256:d035f30e682cfefa3225540753f1c85f14d07bf2109bfde25a5e45d3b53a6928`. Registered in Dolt with seed SQL (`schema/registry/002_wordcount_seed.sql`): account, definition, listing, and reputation snapshot. Full loop verified end-to-end:
>
> - `POST /api/search {"query":"word"}` → finds the tool ✅
> - `POST /api/invoke {"tool":"com.toolshed.tool/word-count@toolshed.dev","input":{"text":"..."}}` → returns `{"words":17,"characters":85,"sentences":2,"paragraphs":1}` with `schema_valid: true` ✅
> - Invocation recorded in Dolt ledger ✅
> - `GET /api/reputation/...` → returns reputation snapshot ✅
> - `POST /api/review` → writes upvote ✅
>
> One bug fixed during integration: ledger schema had `VARCHAR(64)` for hash columns (needed `VARCHAR(71)` to fit `sha256:` prefix + 64 hex chars). Fixed in `schema/ledger/001_init.sql` and migrated live via ALTER TABLE. M3 "It calls" is now fully green.

---

## Step 7 — MCP Server

**Time: 2-3 days.**

```
  apps/mcp-server/

  ┌──────────────────────────────────────────────────────────────────┐
  │  TypeScript — @modelcontextprotocol/sdk                          │
  │                                                                  │
  │  Tool: toolshed_search                                           │
  │  ├── Input:  { capability: string, max_price?: number }          │
  │  ├── Calls:  POST gateway:8080/api/search                        │
  │  └── Output: [ { name, tool_id, price, reputation_score } ]      │
  │                                                                  │
  │  Tool: toolshed_invoke                                           │
  │  ├── Input:  { tool: string, input: object }                     │
  │  ├── Calls:  POST gateway:8080/api/invoke                        │
  │  └── Output: { invocation_id, result, payment_info }             │
  │                                                                  │
  │  Tool: toolshed_reputation                                       │
  │  ├── Input:  { tool: string }                                    │
  │  ├── Calls:  GET  gateway:8080/api/reputation/:tool              │
  │  └── Output: { avg_quality, verified_upvotes, sla_compliance }   │
  │                                                                  │
  │  Tool: toolshed_review                                           │
  │  ├── Input:  { tool: string, quality: number, useful: boolean }  │
  │  ├── Calls:  POST gateway:8080/api/review                        │
  │  └── Output: { upvote_id }                                       │
  └──────────────────────────────────────────────────────────────────┘

  Distribution: npx @agent-toolshed/mcp-server
  ~200 lines of code. Each handler is one HTTP call.
```

**The config that makes it all work:**

```json
{
  "mcpServers": {
    "toolshed": {
      "command": "npx",
      "args": ["@agent-toolshed/mcp-server"],
      "env": {
        "TOOLSHED_GATEWAY_URL": "http://localhost:8080",
        "TOOLSHED_ACCOUNT_ID": "acct_test123"
      }
    }
  }
}
```

**Done when:** You add this to Claude Code, ask "find me a sentiment analysis tool and analyze this text," and it searches the registry, picks a tool, calls it through the gateway, and returns the result. This is the moment the whole thing clicks.

---

## What's Deliberately Out of Scope

Don't build these yet. They're real, they matter, but they're not MVP.

```
  ┌──────────────────────────────┬───────────────────────────────────────┐
  │  OUT OF SCOPE (for now)      │  WHY                                  │
  ├──────────────────────────────┼───────────────────────────────────────┤
  │  Payment processing          │  Prove discovery works first          │
  │  Stripe Connect              │  Not needed until you handle money    │
  │  Credit drawdown / metering  │  No payment = no billing infra        │
  │  Web UI (toolshed.sh)       │  No one needs npmjs.com before npm    │
  │  CLI tool                    │  curl + SQL is fine for now           │
  │  Domain verification         │  Hardcode accounts                    │
  │  DID / AT Protocol           │  Future evolution, not MVP            │
  │  gRPC / MCP protocol adapts  │  REST-only is fine for MVP            │
  │  Reputation materialization  │  Need volume before ranking matters   │
  │  Federation / self-hosting   │  Solve when someone asks for it       │
  │  Decentralized registry      │  Phase 6 — prove centralized first   │
  │  Multi-node / distributed    │  Single node, simple                  │
  └──────────────────────────────┴───────────────────────────────────────┘
```

---

## The Milestones That Matter

```
  M1 ──── "It compiles"
           Go types, content hashing, tests pass.
           You have something to git push.

  M2 ──── "It stores"
           Dolt schema is live, seed data is in,
           you can query tools with SQL.

  M3 ──── "It calls"
           Gateway proxies a request to a real tool
           and returns a real response. Invocation
           is logged in the Dolt ledger.

  M4 ──── "It discovers" ◄──── THE DEMO MOMENT
           Agent adds one config line, searches for
           a tool, finds it, calls it, gets a result.
           Record a screen capture of this.
```

Four milestones. No payment milestone — that's a future concern. The question you're answering is: **does an agent use dynamic tool discovery when it's available?**

---

## Decision Log

Decisions already made. Don't re-litigate these.

| Decision                       | Why                                                                       | Revisit When                                |
| ------------------------------ | ------------------------------------------------------------------------- | ------------------------------------------- |
| Go for the core                | Dolt is Go, single binary, fast builds                                    | Never                                       |
| TypeScript for MCP server only | Official MCP SDK, npx distribution                                        | If Go MCP SDK matures                       |
| REST-only protocol adapter     | Simplest thing that works                                                 | When a real provider needs MCP/gRPC         |
| Single Dolt instance           | No distributed coordination at MVP                                        | When availability requires it               |
| No payment processing          | Prove discovery first, add billing later                                  | When providers want managed payments        |
| Payment info is informational  | Surface provider's Stripe/billing info, don't process it                  | When the platform fee model is validated    |
| No web UI for MVP              | The MCP server IS the interface for agents; curl IS the interface for you | When providers need self-service onboarding |
| Content hash = sha256          | Simple, deterministic, well-understood                                    | Never (it's the identity system)            |
| Two Dolt databases             | Registry (shared) + Ledger (local) from day one                           | Never (it's the trust model)                |

---

## Timeline (Realistic)

```
  Week 1   ████████████████░░░░  Steps 1-4: Foundation
            Repo, types, hashing, Dolt schema
            Lots of small wins. Momentum builder.

  Week 2-3 ████████████████████  Steps 5-6: Gateway + Real Tool
            The hard engineering week. Dolt queries,
            protocol routing, schema validation,
            invocation logging.

  Week 3-4 ████████████████░░░░  Step 7: MCP Server
            Fast if gateway API is clean.
            Demo day at the end of this week.

  Week 5+  ░░░░░░░░░░░░░░░░░░░  Iterate
            Real users, real feedback, real bugs.
            Now you know what to build next.
```

~4 weeks to the demo moment. No payment complexity. No Stripe rabbit holes. Pure focus on: **does the discovery loop work?**

---

## One Last Thing

The design doc is done. The infra doc is done. This plan is done. **The next thing you create should be a `.go` file.**
