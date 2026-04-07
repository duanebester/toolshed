# The Agent Toolshed

**An open registry where companies list tools, agents discover and pay for them, and reputation emerges from verified usage — backed by Dolt DB, powered by Stripe, inspired by AT Protocol's data model.**

---

## The Problem

AI agents are increasingly capable, but when they need specialized tools — fraud detection, geospatial analysis, compliance checking, sentiment analysis — they hit a wall:

- **No discovery**: Tools are hardcoded or manually configured. There's no Yellow Pages for agent capabilities.
- **No payment**: There's no machine-native way to pay for a tool call. It's all API keys, billing dashboards, and enterprise contracts.
- **No reputation**: An agent can't know which tool provider is reliable, fast, or accurate without a human pre-vetting everything.
- **No portability**: Switch your MCP server or API provider and you're rewiring everything.
- **No audit trail**: When an agent makes a decision based on a tool's output, there's no versioned, reproducible record of what happened.

Underneath all of this is a deeper problem: **SaaS pricing models assume a human buyer.** Per-seat licenses, annual contracts, "schedule a demo" funnels, billing dashboards — the entire commercial infrastructure of software-as-a-service was designed for humans who evaluate, negotiate, sign, and manage. When your buyer is an autonomous agent with a budget and a task that lives for 30 seconds, none of it works. An agent can't sit through a sales call. It can't sign an MSA. It can't manage a billing portal. Per-seat pricing makes no sense when your "user" is a subprocess that spins up, calls three tools, and terminates. The unit economics of SaaS — built around human consumption patterns — break down when agents are the customers.

The protocol debate (MCP vs. skills vs. raw REST vs. gRPC) is a distraction. The real gap is: **how do agents find, trust, pay for, and audit tool usage across organizational boundaries?**

---

## Inspirations

| Source                                                                             | What We Take                                                                                                                                                                                              | Key Insight                                                                                                                                                                                                       |
| ---------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [AT Protocol / "A Social Filesystem"](https://overreacted.io/a-social-filesystem/) | Records, lexicons, DIDs, "data outlives software" philosophy, extensibility patterns. **Not** Bluesky's relay/firehose or social graph.                                                                   | _"Our memories, our thoughts, our designs should outlive the software we used to create them."_ Replace "software" with "agent frameworks" and the same principle applies to tools.                               |
| [Gas Town (Steve Yegge)](https://github.com/steveyegge/gastown)                    | Dolt as the backbone for all agent state. Every mutation is a commit. Federation via Dolt remotes. Mol Mall as a marketplace precedent.                                                                   | Dolt gives agents **reproducibility, auditability, and collaboration** on structured data, using branch/merge/diff workflows developers already know from Git.                                                    |
| [Dolt DB](https://www.dolthub.com/blog/2024-10-15-dolt-use-cases/)                 | Git-for-data: `AS OF` queries, `dolt_diff()`, branch/merge, `dolt clone`/`dolt push`, DoltHub for public hosting and PRs.                                                                                 | A SQL database with full version history — time-travel, diffing, forking, and federation come free.                                                                                                               |
| [Unison](https://www.unison-lang.org/docs/the-big-idea/)                           | Content-addressed identity for tool definitions. Two-layer split: immutable definitions (keyed by hash) and mutable names/metadata (pointers to hashes). **Not** Unison's syntax tree hashing or runtime. | _"What we now think of as a dependency conflict is instead just a situation where there are multiple terms or types that serve a similar purpose."_ A "new version" of a tool isn't a mutation — it's a new hash. |
| [Stripe Agent Toolkit (`stripe/ai`)](https://github.com/stripe/ai)                 | `@stripe/token-meter` fire-and-forget metering pattern, Stripe Accounts v2 unified identity, Billing Credits + Billing Meters for micropayments.                                                          | Stripe is building billing for AI token usage. The Toolshed completes the other side — billing for **tool calls**. Same operator, same Stripe Account, one unified financial identity.                            |

---

## The On-Ramp

Today, developers give agents tools by adding MCP servers and API keys to a JSON config file. The Toolshed doesn't change that pattern — it _is_ that pattern. The Toolshed is an MCP server. You add it to your agent's config exactly like any other tool:

```json
{
  "mcpServers": {
    "toolshed": {
      "command": "npx",
      "args": ["@agent-toolshed/mcp-server"],
      "env": { "TOOLSHED_ACCOUNT_ID": "acct_abc123" }
    }
  }
}
```

One config entry. Now your agent has access to every tool in the registry. No new paradigm, no behavioral shift — just another MCP server that happens to be a gateway to thousands of tools. The `TOOLSHED_ACCOUNT_ID` is a Stripe Accounts v2 object — the same ID works whether the operator is consuming tools, providing tools, or both.

### The Meta-Tools

The Toolshed exposes a small set of meta-tools:

- **`toolshed_search`** — find tools by capability, price, latency, reputation
- **`toolshed_invoke`** — call a tool (handles payment, schema validation, invocation logging)
- **`toolshed_reputation`** — check a tool's reliability, quality scores, SLA compliance
- **`toolshed_review`** — submit a proof-of-use upvote after using a tool

### The Flow

```
1. Agent has a task: "analyze this transaction for fraud"
2. Agent doesn't have a fraud tool → calls toolshed_search({ capabilities: ["fraud"], max_price: 0.01 })
3. Toolshed returns ranked results from the Dolt registry
4. Agent picks one → calls toolshed_invoke({ tool: "fraud-detection-v3@acme.com", input: {...} })
5. Gateway: checks credit balance (cached, fast) → routes to provider → on success:
   fires meter event to Stripe (async), records invocation in Dolt, returns result + invocation_id
6. Agent evaluates, optionally calls toolshed_review({ tool: "...", quality: 5, useful: true })
```

An agent that already has a hardcoded fraud tool will never call `toolshed_search` — it'll just use what it has. But the first time it needs something it _doesn't_ have, the Toolshed is right there. Discovery happens organically at the edges. The Toolshed doesn't compete with existing tool configurations — it's additive.

---

## Architecture

### The Three-Layer Split

```
┌────────────────────────────────────────────────────────────┐
│  LAYER 1: REGISTRY (Dolt-backed, shared)                   │
│                                                            │
│  "What exists, who provides it, what's the contract"       │
│  - Tool records (schema, pricing, endpoint, payment, SLA)  │
│  - Company identity (domain-verified, optionally via DIDs) │
│  - Upvotes (proof-of-use quality signals)                  │
│  - Versioned schemas (lexicons)                            │
│  - Capability search and discovery                         │
│  - Clonable by anyone: dolt clone toolshed/registry        │
└───────────────────────┬────────────────────────────────────┘
                        │
┌───────────────────────▼────────────────────────────────────┐
│  LAYER 2: GATEWAY (thin routing + payment + metering)      │
│                                                            │
│  "Invoke the tool, handle payment, verify response"        │
│  - Protocol translation (MCP, REST, gRPC — doesn't care)  │
│  - Credit balance check (cached locally, no Stripe trip)   │
│  - Payment: credit drawdown (fast) or destination charge   │
│  - Metering: fire-and-forget Stripe Billing Meter events   │
│  - Response validation against schema                      │
│  - Stateless for call data — inputs/outputs not stored     │
└───────────────────────┬────────────────────────────────────┘
                        │
┌───────────────────────▼────────────────────────────────────┐
│  LAYER 3: LEDGER (Dolt — the audit trail, local)           │
│                                                            │
│  "Who called what, when, what did it cost"                 │
│  - Every invocation = a Dolt commit                        │
│  - Input hash, output hash, timing, meter event ID         │
│  - Time-travel, diff, reproduce any agent decision         │
│  - The proof layer that metered billing alone can't provide│
│  - Local to each node — never shared or cloned             │
└────────────────────────────────────────────────────────────┘
```

The gateway is **Cloudflare, not AWS** — route the request, move the money, forget the rest. It never stores call inputs, call outputs, or response bodies. This is deliberate: you can't leak data you don't hold, there's no GDPR management of call payloads, and "we don't store your data" is the easiest trust pitch in enterprise sales.

### Protocol Agnosticism

The MCP-vs-skills debate is a false choice. From the Toolshed's perspective, the invocation method is just a field in the tool record:

```json
"invocation": {
    "protocol": "mcp",
    "endpoint": "https://tools.acme.com/mcp",
    "tool_name": "fraud_check"
}
```

The `invocation.protocol` could be `"mcp"`, `"rest"`, `"grpc"`, `"graphql"`, or `"skill"`. The Toolshed doesn't care. It's like how DNS doesn't care what protocol you speak once you've resolved the address. The **schema is the contract**; the protocol is a transport detail.

Companies keep running their tools on their own infrastructure. They don't install our software. They don't change their API. They just publish a record that says: _"here's what I've got, here's how to reach it, here's what it costs, here's how to pay."_

---

## Everything Is Records

Every entity in the system is a **record** — a JSON document with a schema. There are no special servers for payment processing, reputation, or discovery. It's all records in the Dolt registry, with materialized views computed by whoever needs them. This follows the AT Protocol philosophy that data should be portable and owned by its creator, but the Toolshed implements it directly on Dolt rather than requiring AT Protocol infrastructure.

### The Tool Record

A company registers a tool in two parts: an immutable **definition** (the contract) and a mutable **listing** (the metadata).

The registry hashes the definition's schema, invocation, capabilities, and provider identity to produce a `content_hash` — the tool's true identity (inspired by Unison's content-addressed definitions). Names, pricing, and SLA are mutable metadata on the listing that point to the hash.

```json
// Definition (immutable, keyed by content_hash)
// content_hash: sha256:a1b2c3d4e5f6...
// Provider: acme.com (domain-verified)

{
  "provider": {
    "domain": "acme.com",
    "did": "did:plc:acme-corp"
  },

  "schema": {
    "input": {
      "transaction_id": { "type": "string" },
      "amount": { "type": "number" },
      "merchant_category": { "type": "string" }
    },
    "output": {
      "risk_score": { "type": "number", "min": 0, "max": 1 },
      "flags": { "type": "array", "items": { "type": "string" } }
    }
  },

  "invocation": {
    "protocol": "mcp",
    "endpoint": "https://tools.acme.com/mcp",
    "tool_name": "fraud_check"
  },

  "capabilities": ["fraud", "ml", "financial", "real-time"],

  "createdAt": "2026-03-01T00:00:00Z"
}
```

```json
// Listing (mutable, points to a definition)
// Collection: com.toolshed.tool/
// Record key: fraud-detection@acme.com

{
  "definition_hash": "sha256:a1b2c3d4e5f6...",

  "name": "Fraud Detection",
  "version_label": "3.1.0",
  "description": "Real-time transaction fraud scoring with ML",

  "pricing": {
    "model": "per_call",
    "price": 0.005,
    "currency": "usd",
    "bulk": { "1000": 0.004, "10000": 0.0025 }
  },

  "payment": {
    "methods": [
      {
        "type": "stripe_connect",
        "provider_account": "acct_acme_abc123",
        "price_per_call": 0.005,
        "currency": "usd"
      }
    ]
  },

  "sla": {
    "p99_latency_ms": 500,
    "uptime": "99.9%",
    "rate_limit": "1000/min"
  },

  "updatedAt": "2026-03-01T00:00:00Z"
}
```

### Content-Addressed Tools

Inspired by Unison, where every definition is identified by a hash of its syntax tree and names are just metadata. The Toolshed applies this to tools: the **schema + invocation contract + provider identity** are hashed to produce a `content_hash`. This splits every tool into two layers:

- **Tool definitions** (immutable, keyed by content hash) — the contract. Once written, never updated, never deleted.
- **Tool listings** (mutable, human-readable) — the pointer. Name, description, version label, pricing, SLA. Updated freely by the provider.

**What this gets you:**

- **No breaking changes, ever.** New schema → new hash → new definition. Old hash still exists. Agents pinned to the old hash keep working.
- **No version conflicts.** Two definitions with different schemas are just different hashes. They coexist.
- **Agents pin by hash, not by name.** After a successful call, an agent stores `toolshed:sha256:abc123` — immutable, precise.
- **Identical tools deduplicate.** Two providers publishing the same schema and contract share a content hash.
- **Version labels are cosmetic.** `"3.1.0"` is for humans, like a Git tag. The hash is the real identity.

Search operates on the mutable metadata layer — names, descriptions, capabilities, pricing. Each result carries its `content_hash`. Humans browse the registry like npm; the hash is a detail you can click into, like a commit SHA on GitHub.

### The Upvote Record (with Proof of Use)

When an agent uses a tool and gets results, it can create an upvote record — a quality signal with proof that the agent actually used and paid for the tool:

```json
// Caller: agent-company-xyz.com (domain-verified)
// Collection: com.toolshed.tool.upvote/
// Record key: 5kqw3xmops7n2

{
  "subject": "com.toolshed.tool/fraud-detection-v3@acme.com",

  "proof": {
    "payment_method": "stripe_credits",
    "meter_event_id": "mevt_1abc123def456",
    "amount": 0.005,
    "currency": "usd",
    "invocation_hash": "sha256:deadbeef...",
    "ledger_commit": "dolt:76qerj11u38il8rb1ddjn3d6kivqamk2",
    "called_at": "2026-03-15T14:23:00Z"
  },

  "evaluation": {
    "quality": 5,
    "latency_met_sla": true,
    "schema_valid": true,
    "useful": true
  },

  "context": {
    "agent_runtime": "claude-code",
    "task_type": "financial-analysis",
    "input_complexity": "high"
  },

  "createdAt": "2026-03-15T14:23:05Z"
}
```

`schema_valid` and `latency_met_sla` are objectively measurable by the gateway. `quality` and `useful` are the agent's subjective assessment — optional, but valuable when the agent has ground truth to evaluate against.

### Summary of Record Types

| Record          | Where It Lives                               | Purpose                                                        |
| --------------- | -------------------------------------------- | -------------------------------------------------------------- |
| Tool definition | Dolt registry (`tool_definitions`)           | Immutable contract: schema, invocation, capabilities           |
| Tool listing    | Dolt registry (`tool_listings`)              | Mutable metadata: name, pricing, SLA — points to a definition  |
| Upvote          | Dolt registry (`upvotes`)                    | Quality signal with proof-of-use (meter event + ledger commit) |
| Invocation log  | Dolt ledger (`invocations`) — **local only** | Record of each call: hashes, timing, payment proof             |
| Account         | Dolt registry (`accounts`)                   | Provider/operator identity, domain, Stripe Account ID          |
| Reputation      | Dolt registry (`reputation`) — **computed**  | Materialized view aggregated from upvotes and invocations      |

Three record types in the shared layer (definitions, listings, upvotes). One in the local layer (invocations). Accounts and reputation are derived/managed. Nothing else.

---

## Payment

### Philosophy

Payment is not a special subsystem. It's **just a field on the tool record** — the provider declares "send cash this way" as part of their registration. The Toolshed is a **Stripe Connect platform** — a billing intermediary that earns its place by handling all the payment infrastructure so providers and agents don't have to.

This is honestly centralized for MVP. The Toolshed takes a platform fee on every tool call routed through the hosted gateway. But the centralization provides real value (billing, tax, invoicing, single bill for agents, single payout for providers), and the exit is real — anyone can run their own Toolshed instance. And the payment field on the tool record supports multiple methods — including ones that bypass the gateway entirely.

### The Unified Account Model

Stripe's Accounts v2 API lets a single `Account` object carry multiple configurations:

- **Tool provider** → Account with `merchant` configuration (receives payments)
- **Agent operator** → Account with `customer` configuration (has payment method, credit balance, metered subscription)
- **Both** → Account with `merchant` + `customer` configurations (one identity, one account)

The `TOOLSHED_ACCOUNT_ID` in the agent's config is the same ID that appears in the tool record's `provider_account` field. One identity across the whole system.

### The Hybrid Payment Model

| Call type                 | Payment mechanism                                              | Latency      | Trust model                                            |
| ------------------------- | -------------------------------------------------------------- | ------------ | ------------------------------------------------------ |
| Sub-$1 calls (most calls) | **Credit drawdown** — deduct from prepaid balance, meter async | ~0ms added   | Credits are pre-funded; Dolt ledger is the proof layer |
| High-value calls (>$1)    | **Destination charge** — synchronous Stripe charge before call | ~500ms added | Stripe charge ID verifiable by both parties            |
| Free / open-source tools  | **None** — just auth                                           | 0ms          | N/A                                                    |
| L402 / Cashu (future)     | **Direct to provider** — no gateway in the payment loop        | ~0ms added   | Cryptographic proof (preimage / ecash token)           |

The default threshold is **$1**. Providers can override (e.g., always require synchronous proof). The gateway respects the stricter of the two settings.

### Credit Drawdown (The Fast Path)

Stripe's **Billing Credits** plus **Billing Meters** provide the fast-path mechanism, following the same fire-and-forget pattern as Stripe's `@stripe/token-meter` SDK. The operator pre-funds a credit balance. Each tool call draws down credits locally, with async metering to Stripe for invoicing and settlement.

```
1. Agent calls toolshed_invoke({ tool: "fraud-v3@acme.com", input: {...} })
2. Gateway checks credit balance (local cache, no Stripe round-trip)
3. Gateway calls the provider's tool endpoint
4a. SUCCESS → fire meter event to Stripe (async, fire-and-forget)
             → decrement local credit balance cache
             → record invocation in Dolt ledger
             → return result + invocation_id to agent
             → gateway forgets the call data
4b. FAILURE → no meter event fired → no credit drawdown
             → record failure in Dolt ledger → return error
```

The meter event includes an `identifier` field that Stripe deduplicates within a 24-hour window — this provides crash recovery without any custom two-phase commit logic. The Dolt ledger and Stripe meters are complementary proof systems: Stripe handles the money, Dolt handles the receipts, either party can audit by comparing the two.

### Payment Methods on the Tool Record

```json
"payment": {
    "methods": [
        {
            "type": "stripe_connect",
            "provider_account": "acct_acme_abc123",
            "price_per_call": 0.005,
            "currency": "usd"
        }
    ]
}
```

The `platform_fee_pct` is deliberately absent — fees are set by the platform via Stripe's Platform Pricing Tool, not declared by the provider. Free tools just declare `{ "type": "free" }`.

### Extensibility and Future Payment Methods

New payment methods don't require protocol changes — anyone publishes a new lexicon:

```
com.toolshed.defs#paymentStripeConnect  ← MVP (credits + destination charges)
com.toolshed.defs#paymentFree           ← open source / community tools
com.toolshed.defs#paymentLightning      ← machine-native micropayments (no gateway needed)
com.toolshed.defs#paymentCashu          ← bearer token micropayments (no gateway needed)
```

A tool can list multiple payment methods. When the agent uses `stripe_connect`, the Toolshed gateway handles it and takes its cut. When the agent uses `l402` or `cashu`, the agent pays the provider directly — the Toolshed is cut out of the payment entirely. **The Toolshed has to compete on value** (discovery, reputation, convenience) rather than lock-in.

### Failure and Refunds

- **Credit-based calls**: Gateway doesn't fire the meter event → no charge. Failure recorded in Dolt ledger.
- **Destination charges**: Gateway creates a refund with `reverse_transfer=true` and `refund_application_fee=true` — Stripe unwinds atomically.
- **Gray areas**: The Dolt ledger gives both parties an auditable record to resolve disputes. Low-quality upvotes with valid payment proofs are strong public signals.

---

## Distributed Reputation

### How It Works

Reputation is not stored on the tool. It's **derived** — a materialized view computed from upvote records and invocation counts in the Dolt registry:

```
REPUTATION for acme's fraud-detection-v3:

  = Scan all com.toolshed.tool.upvote records
    where subject = "com.toolshed.tool/fraud-detection-v3@acme.com"
    AND proof is valid (meter event checks out, invocation hash exists in ledger)

  = Aggregate quality scores, count verified upvotes, compute percentiles

Nobody owns this score.
Nobody can inflate it without paying for real usage.
Anybody can compute it from the Dolt data (clone the registry and run the query).
```

Upvotes carry both **volume signals** (objective, gateway-measurable: `schema_valid`, `latency_met_sla`, raw invocation counts) and **quality signals** (subjective, agent-reported: `quality` 1-5, `useful` boolean). Both are needed — a tool with 10,000 calls, 99% schema compliance, and an average quality of 2.1 runs reliably but gives bad answers.

### Anti-Gaming Properties

| Attack                   | Why It Fails                                                                                |
| ------------------------ | ------------------------------------------------------------------------------------------- |
| **Fake upvotes** (sybil) | Proof-of-use required. No valid meter event = unverifiable.                                 |
| **Self-upvoting**        | Provider pays themselves real money. Ledger shows `caller == provider` — trivial to filter. |
| **Wash trading**         | Detectable via diversity-of-callers weighting. PageRank-style graph analysis.               |
| **Buying upvotes**       | Requires real usage and real payment — tool still has to deliver quality results.           |
| **Deleting bad reviews** | Impossible. Upvotes live in the reviewer's repo, not the provider's.                        |

### Discovery Algorithms

Because the registry is a Dolt database anyone can clone, anyone can build **discovery algorithms** over the data — "Trending Tools," "Most Reliable," "My Network Trusts," "Best for Financial Analysis," "Budget Picks." Clone the Dolt registry, write your own ranking SQL, expose it as an API. Competition between discovery algorithms improves quality for everyone.

---

## The Dolt Backbone

### Why Dolt

Every table in the registry gets **Git-style version control for free**:

- `SELECT * FROM tool_listings AS OF '2026-03-01'` — what tools existed on March 1st?
- `SELECT * FROM dolt_diff('main~5', 'main', 'tool_listings')` — what changed in the last 5 commits?
- Branch a tool's schema to test changes, merge when validated
- `dolt clone` the registry for offline agent operation
- `dolt push` to DoltHub for public federation
- Pull requests for schema changes — human review before publishing
- Full audit trail: who registered what, when, and every change since

### Data Locality

| Data                                 | Where                                   | Why                                                        |
| ------------------------------------ | --------------------------------------- | ---------------------------------------------------------- |
| Tool catalog (definitions, listings) | **Shared** — Dolt, clonable by anyone   | This is the product. Must be open for the network to work. |
| Accounts (identity, domain)          | **Shared** — Dolt                       | Identity must be portable and verifiable by anyone.        |
| Upvotes (quality signals)            | **Shared** — Dolt, published by callers | Reputation must be computable by anyone from public data.  |
| Invocations (call records)           | **Local** — Dolt, private to each node  | Call metadata stays private. Only hashes stored.           |
| Credit balances                      | **Stripe** — cached locally             | Stripe is the source of truth. Local cache for fast-path.  |
| Meter events                         | **Stripe** — fire-and-forget            | Stripe handles aggregation, invoicing, settlement.         |
| Call inputs/outputs                  | **Nowhere**                             | The Toolshed doesn't store call data. Period.              |

---

## Business Model

The Toolshed is a **Stripe Connect platform for AI tool calls**. The hosted service at toolshed.sh handles discovery, payment, and reputation — and takes a platform fee on every tool call routed through it.

```
┌─────────────────────────────────────────────────────────┐
│  toolshed.sh (the hosted service)                        │
│                                                          │
│  - Public registry (Dolt, clonable by anyone)            │
│  - Hosted gateway (Stripe Connect, volume-tiered fees)   │
│  - Billing Credits, Meters, Platform Pricing Tool        │
│  - Discovery, reputation, all the materialized views     │
│  - You sign up, get a TOOLSHED_ACCOUNT_ID, done          │
│                                                          │
│  "The easy button"                                       │
└──────────────────────────┬──────────────────────────────┘
                           │
                      dolt clone
                           │
┌──────────────────────────▼──────────────────────────────┐
│  your-company's private Toolshed                         │
│                                                          │
│  - Your own registry (fork of public, or from scratch)   │
│  - Your own gateway (your own Stripe, your fees)         │
│  - Internal tools that never touch the public registry   │
│  - You control everything                                │
└─────────────────────────────────────────────────────────┘
```

Nobody complains that GitHub is "centralized" because Git is open and you can leave. Same energy.

### Revenue Streams

- **Platform fee (MVP)**: Volume-tiered application fees — default 15%, scaling to 8% for high-volume. Managed via Stripe's Platform Pricing Tool (no code changes).
- **Credit balance float (MVP)**: Operators pre-fund credit balances. The Toolshed holds the float between credit purchase and provider payout.
- **Premium discovery (growth)**: Promoted placement in `toolshed_search` results. Transparent and labeled.
- **Enterprise self-hosted (later)**: "Run your own Toolshed" with SLA, support, and managed updates.

### Value Props

- **For a tool provider**: "List your tool once, get discovered by every agent. We handle billing. You get a Stripe deposit."
- **For an agent operator**: "One config line, access to every tool. Pre-fund credits, set a budget, done. One bill from Toolshed."
- **For an enterprise**: "Start hosted. When you need control, clone everything and run it yourself. Nothing is trapped."

### The Competitive Moat Over Time

```
Stage 1 (MVP):     Stripe Connect platform fee — we're the billing layer
Stage 2 (Growth):  Network effects — providers and agents concentrate here
Stage 3 (Mature):  L402/Cashu bypass Stripe — we compete on discovery + reputation
Stage 4 (Scale):   The registry IS the moat — like npm, whoever has the packages wins
```

The key tension: as L402/Cashu mature, agents can pay providers directly and cut the Toolshed out of the payment loop entirely. This is by design. The Toolshed must always earn its place by providing value — not by being a required intermediary.

### The Feedback Loop

```
    ┌─── Better tools get more upvotes ◄────────────┐
    │                                                │
    ▼                                                │
More visibility          Agents evaluate results     │
(higher in search,       and create upvote records   │
trending feeds)          (with proof-of-use)         │
    │                            ▲                   │
    ▼                            │                   │
More agents discover     Agents pay and call ────────┘
and choose this tool     the tool
    │                            ▲
    ▼                            │
More revenue for ───────► Provider invests in
the provider              quality / speed / reliability
```

---

## Adoption

### Tier 1: Dolt Registry + Hosted Gateway (MVP)

The lowest-friction path. Submit a JSON tool record to the Dolt registry — via CLI, API, or a PR to a DoltHub repo. Verify domain ownership via DNS TXT record or `.well-known/toolshed.json`. Requires: a JSON file, a domain you own, and a Stripe account.

### Tier 2: AT Protocol Integration (Future Upgrade)

When a company wants portable identity and richer federation, they upgrade to a DID and optionally publish records to a PDS. Domain-verified identity transfers cleanly — AT Protocol already uses domain handles. Existing invocation history and reputation carries over. Both tiers write to the same Dolt tables; agents querying the registry don't care which tier a tool uses.

---

## Mental Model

| Existing System       | Toolshed Equivalent                                                       |
| --------------------- | ------------------------------------------------------------------------- |
| DNS                   | Tool discovery — resolve a capability to an endpoint                      |
| TLS certificates      | Domain verification / DIDs — verify you're talking to who you think       |
| npm registry          | Tool registry — search, discover, version                                 |
| npmjs.com             | toolshed.sh — the hosted service, the "easy button"                       |
| Verdaccio             | Self-hosted Toolshed — same software, your infrastructure                 |
| Unison hashes         | Content-addressed tool definitions — no version conflicts by construction |
| App Store ratings     | Reputation — but only from verified purchasers with proof-of-use          |
| Stripe Connect        | Payment — platform handles billing, provider gets deposited directly      |
| `@stripe/token-meter` | `@toolshed/call-meter` — fire-and-forget usage metering                   |
| Cloudflare            | The gateway — routes traffic, doesn't store your data                     |
| Git + GitHub          | Dolt + DoltHub — version control for the registry and ledger              |

Or more concisely: **npm + Stripe Connect + Dolt, for AI agent tool calls, with AT Protocol's data philosophy and Stripe's metering infrastructure.**

### What This Is Not

- **Not a runtime**: Tools run on the provider's infrastructure. The Toolshed doesn't execute anything.
- **Not a data store**: The gateway is stateless for call data. Only hashes in the invocation ledger.
- **Not an MCP replacement**: MCP, REST, gRPC are invocation protocols. The Toolshed is discovery, payment, and reputation. Complementary.
- **Not a blockchain**: Dolt has Git semantics — versioned and auditable, but not distributed consensus. Federation via Dolt remotes.
- **Not a Bluesky app**: Uses AT Protocol's design patterns but does not depend on Bluesky's infrastructure.
- **Not inescapably centralized**: The hosted service takes a cut. The registry is a Dolt database anyone can clone. The gateway is open source anyone can run.

---

## Open Questions

- **Lexicon governance**: Who defines `com.toolshed.*` lexicons? A foundation? A GitHub org? Follow AT Protocol norms — publish early, evolve carefully, let the ecosystem fork if needed.
- **Settlement cadence**: When operators pre-fund credits, the platform holds the float until provider payout. Daily? Weekly? Real-time? Provider trust correlates with payout speed.
- **Credit balance cache at scale**: MVP is single-node with a local cache — no double-spend risk. Multi-node needs sticky routing or a shared cache with distributed locking. Scale problem, not a v1 problem.
- **Stripe meter event rate limits**: If we hit Stripe's rate limits at scale, batch meter events or introduce a local fast-path ledger (TigerBeetle) with periodic Stripe settlement.
- **Upvote adoption**: Tools with lots of usage but few upvotes have a visibility gap. Consider publishing invocation counts as a lightweight volume signal alongside upvotes.
- **Agent review quality**: Early review quality will be mostly binary (worked vs. didn't). Make `schema_valid` and `latency_met_sla` the primary automated signals; treat `quality` and `useful` as optional enrichment.
- **Dispute resolution**: Clear-cut failures trigger automatic no-charge. Gray areas need the Dolt ledger + Stripe meter reconciliation for auditable evidence. Formal arbitration is still an open design problem.

---

## Prior Art and References

- **AT Protocol**: https://atproto.com/guides/overview
- **"A Social Filesystem" (Dan Abramov)**: https://overreacted.io/a-social-filesystem/
- **Gas Town (Steve Yegge)**: https://github.com/steveyegge/gastown
- **Gas Town Dolt Storage Architecture**: https://github.com/steveyegge/gastown/blob/main/docs/design/dolt-storage.md
- **Gas Town Mol Mall Design**: https://github.com/steveyegge/gastown/blob/main/docs/design/mol-mall-design.md
- **Gas Town Federation**: https://github.com/steveyegge/gastown/blob/main/docs/design/federation.md
- **Dolt DB**: https://docs.dolthub.com/introduction/what-is-dolt
- **Dolt Use Cases**: https://www.dolthub.com/blog/2024-10-15-dolt-use-cases/
- **Unison Programming Language — The Big Idea**: https://www.unison-lang.org/docs/the-big-idea/
- **L402 (HTTP 402 + Lightning)**: https://docs.lightning.engineering/the-lightning-network/l402
- **Cashu (ecash)**: https://cashu.space
- **Bluesky Lexicons**: https://atproto.com/guides/lexicon
- **pdsls (AT Protocol file browser)**: https://pdsls.dev
- **Stripe Agent Toolkit (stripe/ai)**: https://github.com/stripe/ai
- **Stripe `@stripe/token-meter`**: https://github.com/stripe/ai/tree/main/llm/token-meter
- **Stripe `@stripe/ai-sdk`**: https://github.com/stripe/ai/tree/main/llm/ai-sdk
- **Stripe MCP Server**: https://mcp.stripe.com
- **Stripe Accounts v2 API**: https://docs.stripe.com/connect/accounts-v2
- **Stripe Billing Credits**: https://docs.stripe.com/billing/subscriptions/usage-based/billing-credits
- **Stripe Billing Meters**: https://docs.stripe.com/billing/subscriptions/usage-based/recording-usage
- **Stripe Meter Events API**: https://docs.stripe.com/api/billing/meter-event
- **Stripe Platform Pricing Tool**: https://docs.stripe.com/connect/platform-pricing-tools
- **Stripe Destination Charges**: https://docs.stripe.com/connect/destination-charges

---

## Appendix A: Registry Schema (SQL)

```sql
-- Accounts (unified identity via Stripe Accounts v2)
CREATE TABLE accounts (
    id VARCHAR(255) PRIMARY KEY,            -- acct_abc123 (Stripe Account v2 ID)
    domain VARCHAR(255) NOT NULL,           -- acme.com (verified via DNS TXT or .well-known)
    did VARCHAR(255),                       -- did:plc:acme-corp (optional, for portable identity)
    display_name VARCHAR(255),
    is_provider BOOLEAN DEFAULT FALSE,
    is_operator BOOLEAN DEFAULT FALSE,
    stripe_onboarded BOOLEAN DEFAULT FALSE,
    created_at DATETIME,
    updated_at DATETIME
);

-- Tool definitions (immutable, content-addressed)
CREATE TABLE tool_definitions (
    content_hash VARCHAR(64) PRIMARY KEY,    -- sha256 of (schema + invocation + provider identity)
    provider_account VARCHAR(255) NOT NULL,
    provider_domain VARCHAR(255) NOT NULL,
    provider_did VARCHAR(255),
    schema_json JSON NOT NULL,
    invocation_json JSON NOT NULL,
    capabilities JSON,
    created_at DATETIME,
    FOREIGN KEY (provider_account) REFERENCES accounts(id)
);

-- Tool listings (mutable, human-readable metadata)
CREATE TABLE tool_listings (
    id VARCHAR(255) PRIMARY KEY,             -- com.toolshed.tool/fraud-detection@acme.com
    definition_hash VARCHAR(64) NOT NULL,
    provider_account VARCHAR(255) NOT NULL,
    provider_domain VARCHAR(255) NOT NULL,
    provider_did VARCHAR(255),
    name VARCHAR(255) NOT NULL,
    version_label VARCHAR(32),
    description TEXT,
    pricing_json JSON NOT NULL,
    payment_json JSON NOT NULL,
    sla_json JSON,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (definition_hash) REFERENCES tool_definitions(content_hash),
    FOREIGN KEY (provider_account) REFERENCES accounts(id)
);

-- Upvotes (proof-of-use quality signals — shared)
CREATE TABLE upvotes (
    id VARCHAR(255) PRIMARY KEY,
    tool_id VARCHAR(255) NOT NULL,
    caller_account VARCHAR(255) NOT NULL,
    caller_domain VARCHAR(255) NOT NULL,
    caller_did VARCHAR(255),
    quality_score INT,
    latency_met_sla BOOLEAN,
    schema_valid BOOLEAN,
    useful BOOLEAN,
    proof_json JSON NOT NULL,
    context_json JSON,
    created_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id),
    FOREIGN KEY (caller_account) REFERENCES accounts(id)
);

-- Invocations (local to each node — never shared or cloned)
CREATE TABLE invocations (
    id VARCHAR(255) PRIMARY KEY,
    tool_id VARCHAR(255) NOT NULL,
    definition_hash VARCHAR(64) NOT NULL,
    caller_account VARCHAR(255) NOT NULL,
    caller_domain VARCHAR(255) NOT NULL,
    input_hash VARCHAR(64) NOT NULL,
    output_hash VARCHAR(64),
    payment_method VARCHAR(32),
    payment_amount DECIMAL(10,4),
    payment_currency VARCHAR(16),
    meter_event_id VARCHAR(255),
    latency_ms INT,
    schema_valid BOOLEAN,
    success BOOLEAN,
    created_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id),
    FOREIGN KEY (caller_account) REFERENCES accounts(id)
);

-- Reputation (computed, cached — materialized view of upvotes)
CREATE TABLE reputation (
    tool_id VARCHAR(255) PRIMARY KEY,
    total_upvotes INT DEFAULT 0,
    verified_upvotes INT DEFAULT 0,
    avg_quality DECIMAL(3,2),
    sla_compliance_pct DECIMAL(5,2),
    schema_compliance_pct DECIMAL(5,2),
    unique_callers INT DEFAULT 0,
    total_invocations INT DEFAULT 0,
    trend VARCHAR(16),
    computed_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id)
);
```

---

## Appendix B: Detailed Stripe Flows

### Operator Onboarding (One-Time Setup)

```
1. Toolshed creates a Stripe Account v2 with `customer` configuration
2. Creates a Subscription with a metered Price tied to the "toolshed_tool_calls" Meter
3. Operator pre-funds a Credit Grant (e.g., $50 of prepaid tool-call credits)
4. Gateway caches the Credit Balance Summary for fast local lookups
5. Operator gets a TOOLSHED_ACCOUNT_ID for their agent config
```

### Provider Onboarding

```
1. Provider signs up at toolshed.sh
2. Toolshed creates a Stripe Account v2 with `merchant` configuration
3. Provider completes Stripe onboarding (identity, bank account)
4. Provider lists their tool in the Dolt registry
5. Tool record's payment field references their Account ID
6. They verify domain ownership (DNS TXT record or .well-known)
7. That's it. No SDK. No middleware. No infrastructure changes.
```

If a company is both provider and operator, it's one Account with both configurations. No duplication.

### Stripe Accounts v2 Example

```json
{
  "contact_email": "eng@acme.com",
  "display_name": "Acme Corp",
  "identity": {
    "business_details": { "registered_name": "Acme Corp" },
    "country": "us",
    "entity_type": "company"
  },
  "configuration": {
    "customer": {},
    "merchant": {
      "capabilities": {
        "card_payments": { "requested": true }
      }
    }
  }
}
```

### Destination Charges (High-Value Path)

For calls above the threshold (default: $1), or when a provider requires synchronous proof:

```
1. Gateway creates a destination charge:
   - Charges the agent's operator
   - Routes funds to the provider's Connected Account
   - Application fee flows back to the platform
2. Stripe returns a charge ID — verifiable by both parties
3. Gateway calls the provider's tool endpoint with the charge ID
4. Provider can verify the charge in their Stripe dashboard
```

### Platform Fee Tiers

Managed via Stripe's Platform Pricing Tool — no code changes:

```
Default pricing:            15% application fee on all tool calls
Pricing group "scale":      10% for providers with >10k calls/month
Pricing group "enterprise":  8% for contracted enterprise providers
Pricing group "community":   0% for open-source / free-tier tools
```

### Payment Verification

| Party               | What they verify               | How                                                                         |
| ------------------- | ------------------------------ | --------------------------------------------------------------------------- |
| **Gateway**         | Operator can pay               | Valid payment method + sufficient credit balance + under budget             |
| **Gateway**         | Call succeeded before metering | Tool returned valid response; meter event reported to Stripe                |
| **Provider**        | They got paid                  | Destination transfers in Stripe dashboard; meter summaries match            |
| **Provider**        | Specific call was paid for     | Invocation record in Dolt ledger matches meter event ID                     |
| **Upvote verifier** | Upvote is backed by real usage | `proof.meter_event_id` resolves to a real meter event; ledger commit exists |

### Credit Balance Cache Strategy

```
- Populated on first call per operator (Credit Balance Summary API)
- Decremented locally on each successful metered call (optimistic)
- Replenished via Stripe webhook on new Credit Grant
- Reconciled periodically against Stripe's actual balance
- Single-node constraint for MVP (no distributed cache = no double-spend risk)
- If local cache and Stripe diverge beyond a threshold, gateway pauses and re-syncs
```

### Crash Recovery

```
- Gateway crashes BEFORE meter event → no charge (call never metered)
- Gateway crashes AFTER meter event, BEFORE Dolt commit →
    Retry Dolt commit; meter event is deduplicated via `identifier`
- Gateway crashes AFTER both → clean state, nothing to recover
- No orphaned holds, no timeout logic, no two-phase commit
```

### The `@stripe/token-meter` Parallel

```
// @stripe/token-meter pattern (for LLM tokens):
const meter = createTokenMeter(stripeApiKey);
const response = await openai.chat.completions.create({ ... });
meter.trackUsage(response, 'cus_xxxxx');

// Toolshed pattern (for tool calls):
const meter = createToolCallMeter(stripeApiKey);
const result = await routeToProvider(toolCall);
meter.trackToolCall({
  operatorId: 'acct_operator',
  toolId: 'fraud-detection-v3@acme.com',
  providerAccount: 'acct_acme',
  amount: 500,
  definitionHash: 'sha256:a1b2c3...',
  invocationId: 'inv_unique_123',  // idempotent identifier
});
```

### Future Payment Methods Example

```json
"payment": {
    "methods": [
        {
            "type": "stripe_connect",
            "provider_account": "acct_acme_abc123",
            "price_per_call": 0.005,
            "currency": "usd"
        },
        {
            "type": "l402",
            "endpoint": "https://tools.acme.com/l402/fraud_check",
            "price_sats": 50
        },
        {
            "type": "cashu",
            "mint": "https://mint.acme.com",
            "price_sats": 50
        }
    ]
}
```
