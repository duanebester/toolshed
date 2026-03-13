# The Agent Toolbox

**A decentralized registry where companies list tools, agents discover and pay for them, and reputation emerges from verified usage — backed by Dolt DB, inspired by AT Protocol's data model.**

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

This idea is a convergence of three technologies that each solve a piece of the puzzle.

### AT Protocol Design Patterns / "A Social Filesystem" (Dan Abramov)

Dan Abramov's article reframes the AT Protocol as a **distributed filesystem for social computing**. The Toolbox borrows several of its design patterns — but is not a Bluesky application and does not depend on Bluesky's infrastructure:

- **Everything is a record** — entities are JSON records in repositories, organized into namespaced collections. The Toolbox applies this to tool registrations, upvotes, and invocation logs.
- **Lexicons** are machine-readable schemas that define record formats. Any developer can publish one. No committee approval needed. The Toolbox uses this pattern for tool schemas.
- **DIDs** (decentralized identifiers) provide portable, self-sovereign identity. The Toolbox uses DIDs for provider and caller identity, verified via domain ownership.
- **Apps are materialized views** of distributed data. The Toolbox registry is a materialized view of tool records, computed from the Dolt backbone.
- **"Third party is first party."** Anyone can build discovery algorithms, reputation views, or alternative registries over the same data.

**What we take**: The data model (records, lexicons, DIDs), the philosophy (data outlives software), and the extensibility patterns.

**What we don't take**: Bluesky's relay/firehose infrastructure, the social graph, or any dependency on Bluesky-the-product. Interoperability with Bluesky or other AT Protocol apps is _possible_ because the primitives are shared, but it's not a goal.

**Key insight**: _"Our memories, our thoughts, our designs should outlive the software we used to create them."_ Replace "software" with "agent frameworks" and the same principle applies to tools.

Reference: https://overreacted.io/a-social-filesystem/

### Gas Town (Steve Yegge)

Gas Town is a multi-agent orchestration system for coordinating 20-30+ Claude Code agents working simultaneously. Its architecture provides patterns directly applicable to an agent tool economy:

- **Dolt as the backbone**: Every piece of agent state — tasks ("beads"), messages, assignments, lifecycle — lives in a Dolt database. Every mutation is a commit. Agents can time-travel, diff, merge, and audit everything.
- **Mol Mall (planned)**: A marketplace for "formulas" — reusable agent workflow templates. Think npm for agent workflows. Companies publish formulas; other installations install and run them.
- **Federation**: Multiple Gas Town instances reference each other's work via Dolt remotes and a `hop://` URI scheme for cross-organization discovery.
- **Agent identity and provenance**: Every agent operation is attributed. Git commits carry agent identity. Events are logged with actor information.
- **The Dolt lifecycle**: A six-stage data lifecycle (CREATE → LIVE → CLOSE → DECAY → COMPACT → FLATTEN) manages the growth of versioned data.

**Key insight**: Dolt's version control gives agents something databases never could — **reproducibility, auditability, and collaboration** on structured data, using the same branch/merge/diff workflow developers already know from Git.

Reference: https://github.com/steveyegge/gastown

### Dolt DB

Dolt is a SQL database you can fork, clone, branch, merge, push, and pull just like a Git repository. It's MySQL-compatible — connect with any MySQL client — but every write is a commit with full history.

Production use cases that map directly to the Toolbox:

- **Game studios** (Scorewarrior) branch and merge game configuration data — parallels tool schema versioning.
- **ML training data** (Flock Safety) uses time-travel for reproducibility — parallels reproducing agent decisions.
- **Medical data** (Turbine) uses PR workflows for data changes — parallels tool schema review processes.
- **Content management** (Threekit) uses parallel branches for large teams — parallels multiple providers evolving tools simultaneously.
- **Compliance** (SOX) gets audit trails for free — parallels tool usage auditing.

**Key capabilities for the Toolbox**:

| Feature                    | Application                                                       |
| -------------------------- | ----------------------------------------------------------------- |
| `dolt_history_*` tables    | Full row-level history of every tool registration and invocation  |
| `AS OF` queries            | "What tools were available at time T? What schema was version N?" |
| `dolt_diff()`              | "What changed between schema versions?"                           |
| Branch and merge           | A/B test new pricing, preview schema changes before publishing    |
| `dolt clone` / `dolt push` | Distribute the registry, federate across organizations            |
| DoltHub                    | Public registry hosting, pull requests for schema changes         |
| Signed commits             | Tamper-evident audit trail                                        |

Reference: https://www.dolthub.com/blog/2024-10-15-dolt-use-cases/

### Unison Programming Language

Unison is a programming language where every definition is identified by a **hash of its syntax tree**, not by its name. Names are just metadata — pointers to hashes. This eliminates an entire class of problems:

- **No version conflicts**: Two versions of a function are just two different hashes. They coexist. Old code referencing the old hash keeps working.
- **No builds break**: Dependencies are pinned by hash, not by name. Changing a name doesn't invalidate anything.
- **No dependency diamond**: Different libraries depending on different versions of the same thing? Not a problem — they're different hashes.
- **Immutable definitions**: Once a definition exists at a hash, it never changes. The contents of an address are forever.

**Key insight**: _"What we now think of as a dependency conflict is instead just a situation where there are multiple terms or types that serve a similar purpose."_ The Toolbox applies this to tool schemas — a "new version" isn't a mutation, it's a new hash. The old one still works.

**What we take**: Content-addressed identity for tool definitions. The two-layer split between immutable definitions (keyed by hash) and mutable names/metadata (pointers to hashes).

**What we don't take**: Unison's syntax tree hashing, its codebase manager, or its runtime. We hash the tool's schema + invocation contract + provider identity, using the principle but not the implementation.

Reference: https://www.unison-lang.org/docs/the-big-idea/

---

## The On-Ramp

Today, developers give agents tools by adding MCP servers and API keys to a JSON config file. The Toolbox doesn't change that pattern — it _is_ that pattern. The Toolbox is an MCP server. You add it to your agent's config exactly like any other tool:

```json
{
  "mcpServers": {
    "toolbox": {
      "command": "npx",
      "args": ["@agent-toolbox/mcp-server"],
      "env": { "TOOLBOX_ACCOUNT_ID": "acct_abc123" }
    }
  }
}
```

One config entry. Now your agent has access to every tool in the registry. No new paradigm, no behavioral shift — just another MCP server that happens to be a gateway to thousands of tools. The `TOOLBOX_ACCOUNT_ID` is a Stripe Accounts v2 object — the same ID works whether the operator is consuming tools, providing tools, or both.

### The Meta-Tools

The Toolbox exposes a small set of meta-tools:

- **`toolbox_search`** — find tools by capability, price, latency, reputation
- **`toolbox_invoke`** — call a tool (handles payment, schema validation, invocation logging)
- **`toolbox_reputation`** — check a tool's reliability, quality scores, SLA compliance
- **`toolbox_review`** — submit a proof-of-use upvote after using a tool

### The Flow

```
1. Agent has a task: "analyze this transaction for fraud"
2. Agent doesn't have a fraud tool in its configured tools
3. Agent calls toolbox_search({ capabilities: ["fraud"], max_price: 0.01 })
4. Toolbox returns ranked results from the Dolt registry
5. Agent picks one, calls toolbox_invoke({ tool: "fraud-detection-v3@acme.com", input: {...} })
6. Toolbox gateway handles Stripe payment, calls acme's endpoint, validates response
7. Agent gets the result, uses it for its task
8. Agent calls toolbox_review({ tool: "...", quality: 5, useful: true })
```

### Why This Matters for Adoption

An agent that already has a hardcoded fraud tool will never call `toolbox_search` for fraud — it'll just use what it has. But the first time it needs something it doesn't have, the Toolbox is right there. Discovery happens organically at the edges, exactly where the current model breaks down.

This means the Toolbox doesn't compete with existing tool configurations. It's additive. Developers don't rip out their current setup — they add one line to their config and their agent gains the ability to reach beyond its pre-configured tools when the task demands it.

---

## Architecture

### The Three-Layer Split

```
┌────────────────────────────────────────────────────────────┐
│  LAYER 1: REGISTRY (Dolt-backed)                           │
│                                                            │
│  "What exists, who provides it, what's the contract"       │
│  - Tool records (schema, pricing, endpoint, payment, SLA)  │
│  - Company identity (domain-verified, optionally via DIDs) │
│  - Versioned schemas (lexicons)                            │
│  - Capability search and discovery                         │
└───────────────────────┬────────────────────────────────────┘
                        │
┌───────────────────────▼────────────────────────────────────┐
│  LAYER 2: GATEWAY (thin routing + payment + metering)      │
│                                                            │
│  "Invoke the tool, handle payment, verify response"        │
│  - Protocol translation (MCP, REST, gRPC — doesn't care)  │
│  - Payment: credit drawdown (fast) or destination charge   │
│  - Usage metering via Stripe Billing Meters (async)        │
│  - Credit balance checks (local, no Stripe round-trip)     │
│  - Response validation against schema                      │
│  - Stripe Agent Toolkit for payment orchestration          │
└───────────────────────┬────────────────────────────────────┘
                        │
┌───────────────────────▼────────────────────────────────────┐
│  LAYER 3: LEDGER (Dolt — the audit trail)                  │
│                                                            │
│  "Who called what, when, what did it cost, what            │
│   was the result"                                          │
│  - Every invocation = a Dolt commit                        │
│  - Time-travel, diff, reproduce any agent decision         │
│  - Settlement and reconciliation records                   │
│  - The proof layer that metered billing alone can't provide│
└────────────────────────────────────────────────────────────┘
```

### Protocol Agnosticism

The MCP-vs-skills debate is a false choice. From the Toolbox's perspective, the invocation method is just a field in the tool record:

```json
"invocation": {
    "protocol": "mcp",
    "endpoint": "https://tools.acme.com/mcp",
    "tool_name": "fraud_check"
}
```

The `invocation.protocol` could be `"mcp"`, `"rest"`, `"grpc"`, `"graphql"`, or `"skill"`. The Toolbox doesn't care. It's like how DNS doesn't care what protocol you speak once you've resolved the address. The **schema is the contract**; the protocol is a transport detail.

Companies keep running their tools on their own infrastructure. They don't install our software. They don't change their API. They just publish a record that says: _"here's what I've got, here's how to reach it, here's what it costs, here's how to pay."_

---

## Everything Is Records

Every entity in the system is a **record** — a JSON document with a schema. There are no special servers for payment processing, reputation, or discovery. It's all records in the Dolt registry, with materialized views computed by whoever needs them. This follows the AT Protocol philosophy that data should be portable and owned by its creator, but the Toolbox implements it directly on Dolt rather than requiring AT Protocol infrastructure.

### The Tool Record

A company registers a tool in two parts: an immutable **definition** (the contract) and a mutable **listing** (the metadata). In the simplest case (Tier 1 — see [Adoption Tiers](#adoption-tiers)), this is as easy as a CLI command or a PR to a DoltHub repo. No PDS required.

The registry hashes the definition's schema, invocation, capabilities, and provider identity to produce a `content_hash` — the tool's true identity. Names, pricing, and SLA are mutable metadata on the listing.

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
// Collection: com.toolbox.tool/
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

### The Upvote Record (with Proof of Use)

When an agent uses a tool and gets good results, it creates an upvote record — a quality signal with proof that the agent actually used and paid for the tool:

```json
// Caller: agent-company-xyz.com (domain-verified)
// Collection: com.toolbox.tool.upvote/
// Record key: 5kqw3xmops7n2

{
  "subject": "com.toolbox.tool/fraud-detection-v3@acme.com",

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

### Summary of Record Types

| Record              | Where It Lives                               | Purpose                                                       |
| ------------------- | -------------------------------------------- | ------------------------------------------------------------- |
| Tool definition     | Dolt registry (`tool_definitions`)           | Immutable contract: schema, invocation, capabilities          |
| Tool listing        | Dolt registry (`com.toolbox.tool/`)          | Mutable metadata: name, pricing, SLA — points to a definition |
| Tool schema/lexicon | Dolt registry (`com.toolbox.lexicon/`)       | Machine-readable input/output contract                        |
| Invocation log      | Dolt ledger (`com.toolbox.tool.invocation/`) | Record of each call (input hash, output hash, timing)         |
| Upvote              | Dolt registry (`com.toolbox.tool.upvote/`)   | Quality signal with proof-of-use                              |
| Payment proof       | Field on the upvote record                   | Stripe charge ID — verifiable by both parties                 |

### Content-Addressed Tools (Inspired by Unison)

The Unison programming language never has version conflicts because every definition is identified by a **hash of its syntax tree**. Names are just metadata — pointers to hashes. Two versions of the same function coexist as two different hashes. Old code referencing the old hash keeps working. New code references the new hash. There's nothing to break.

The Toolbox applies this principle to tool records. Instead of relying on a mutable `version` field and human coordination, the tool's **schema + invocation contract + provider identity** are hashed to produce a `content_hash` — the tool's true identity. Names, descriptions, version labels, pricing, and SLA are mutable metadata that point to the hash.

This splits the tool record into two layers:

- **Tool definitions** (immutable, keyed by content hash) — the contract. Schema, invocation endpoint, capabilities, provider identity. Once written, never updated, never deleted.
- **Tool listings** (mutable, human-readable) — the pointer. Name, description, version label, pricing, SLA, payment methods. Updated freely by the provider. Points to a definition hash.

**What this gets you:**

- **No breaking changes, ever.** Provider pushes a new schema → new hash → new definition. Old hash still exists. Agents pinned to the old hash keep working.
- **No version conflicts.** Two definitions with different schemas are just different hashes. They coexist. No coordination needed.
- **Agents pin by hash, not by name.** After a successful call, an agent stores `toolbox:sha256:abc123` — immutable, precise. The name can change, the provider can restructure — the hash is stable.
- **Identical tools deduplicate.** Two providers publishing the same schema and contract share a content hash. Discovery surfaces both providers for the same definition.
- **No `deprecated` field needed.** Old hashes just exist. If no one calls them, reputation goes stale and agents naturally migrate to newer definitions.
- **Version labels are cosmetic.** `"3.1.0"` is for humans, like a Git tag. The hash is the real identity.

**How search and human browsing work:**

Search operates on the mutable metadata layer — names, descriptions, capabilities, pricing. `toolbox_search({ capabilities: ["fraud"], max_price: 0.01 })` returns human-readable results ranked by quality and price. Each result carries its `content_hash`. Humans browse the registry like npm — names, descriptions, version labels. The hash is a detail you can click into, like a commit SHA on GitHub.

**How Dolt makes this seamless:**

The immutable `tool_definitions` table is append-only — Dolt's version history tracks when each definition was added, but the rows themselves never change. The mutable `tool_listings` table gets full Dolt history — `dolt_history_tool_listings` shows every pointer change over time. `AS OF` queries reproduce exactly which definition a listing pointed to on any date. `dolt_diff()` shows what changed between listing updates. Branching lets providers preview schema changes before publishing.

Reference: https://www.unison-lang.org/docs/the-big-idea/

---

## Payment

### Philosophy

Payment is not a special subsystem. It's **just a field on the tool record** — the provider declares "send cash this way" as part of their tool registration. The Toolbox is a **Stripe Connect platform** — a billing intermediary that earns its place in the middle by handling all the payment infrastructure so providers and agents don't have to.

This is honestly centralized for MVP. The Toolbox takes a platform fee on every tool call routed through the hosted gateway. But the centralization provides real value (billing, tax, invoicing, single bill for agents, single payout for providers), and the exit is real — anyone can run their own Toolbox instance and handle payment however they want. And the payment field on the tool record supports multiple methods — including ones that bypass the gateway entirely.

### The Unified Account Model (Stripe Accounts v2)

Stripe's Accounts v2 API lets a single `Account` object carry multiple configurations. The Toolbox uses this to create a **unified identity** for every participant:

- **Tool provider** → Account with `merchant` configuration (receives payments via destination charges)
- **Agent operator** → Account with `customer` configuration (has payment method, credit balance, metered subscription)
- **Both** → Account with `merchant` + `customer` configurations (a company that provides tools AND consumes tools — one identity, one account)

```json
// Creating a Toolbox account via Stripe Accounts v2
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

No more managing separate `Customer` and `Account` objects. No more mapping IDs. The `TOOLBOX_ACCOUNT_ID` in the agent's config is the same ID that appears in the tool record's `provider_account` field. One identity across the whole system.

### The Hybrid Payment Model

The MVP uses a **hybrid model** that picks the right payment mechanism based on the call:

| Call type                 | Payment mechanism                                              | Latency      | Trust model                                            |
| ------------------------- | -------------------------------------------------------------- | ------------ | ------------------------------------------------------ |
| Sub-$1 calls (most calls) | **Credit drawdown** — deduct from prepaid balance, meter async | ~0ms added   | Credits are pre-funded; Dolt ledger is the proof layer |
| High-value calls (>$1)    | **Destination charge** — synchronous Stripe charge before call | ~500ms added | Stripe charge ID verifiable by both parties            |
| Free / open-source tools  | **None** — just auth                                           | 0ms          | N/A                                                    |
| L402 / Cashu (future)     | **Direct to provider** — no gateway in the payment loop        | ~0ms added   | Cryptographic proof (preimage / ecash token)           |

This solves the latency problem that a pure "charge before every call" model creates. Most tool calls are sub-dollar micropayments — you don't need a synchronous Stripe round-trip for a $0.005 fraud check. You need a prepaid balance and a reliable ledger.

The default threshold is **$1** — calls below it use credit drawdown, calls above use a synchronous destination charge. Providers can override this in their tool record (e.g., a high-value compliance tool might always require synchronous proof; a high-trust provider might accept credits for any amount). The gateway respects the stricter of the two settings.

### Credit Drawdown (The Fast Path)

Stripe's **Billing Credits** (public preview) plus **Billing Meters** provide the fast-path payment mechanism. The agent's operator pre-funds a credit balance. Each tool call draws down credits locally, with async metering to Stripe for invoicing and settlement.

**How it works:**

```
1. Agent operator pre-funds credits:
   - Stripe creates a Credit Grant on their Account
   - e.g., $50 of prepaid tool-call credits
   - Credits can have expiration dates, priority, scope

2. Agent calls toolbox_invoke({ tool: "fraud-v3@acme.com", input: {...} })

3. Gateway checks credit balance (local cache, no Stripe round-trip):
   - Sufficient balance? → proceed
   - Insufficient? → reject with "top up credits" error
   - Over per-call budget? → reject with budget exceeded

4. Gateway calls the provider's tool endpoint

5. On success:
   - Gateway reports a meter event to Stripe (async):
     POST /v1/billing/meter_events
     { meter: "tool_calls", payload: { tool_id, amount, provider_account } }
   - Credits draw down at invoice finalization
   - Dolt ledger records the invocation (the proof layer)

6. On failure:
   - No meter event reported → no credit drawdown
   - Dolt ledger records the failure
```

**Why this works for trust:**

The original concern with metered billing was valid: "the provider has no proof of payment at call time." Pure metered billing in a decentralized model lets the gateway lie. The hybrid model solves this:

- **The Dolt ledger is the proof layer.** Every invocation is a commit — input hash, output hash, timing, meter event ID. The provider can clone the ledger and verify.
- **Credits are real money, pre-funded.** The operator already paid Stripe. The question is only allocation, not whether payment exists.
- **Meter events are auditable.** Stripe's meter event summaries reconcile against the Dolt ledger. Any discrepancy is detectable by either party.
- **Settlement is automatic.** Stripe aggregates meter events into invoices. Destination transfers flow to providers on their payout schedule.

### Destination Charges (The High-Value Path)

For calls above a configurable threshold (default: $1), or when a provider requires synchronous payment proof, the gateway falls back to a **Stripe Connect destination charge** — the same model from the original design:

```
1. Gateway creates a destination charge:
   - Charges the agent's operator
   - Routes funds to the provider's Connected Account
   - Application fee flows back to the platform
2. Stripe returns a charge ID — verifiable by both parties
3. Gateway calls the provider's tool endpoint with the charge ID
4. Provider can verify the charge in their Stripe dashboard
```

The `application_fee_amount` on destination charges handles the platform's cut. Stripe deducts its processing fees from the platform's portion, not the provider's — so the provider always receives the full transfer amount minus the application fee.

### Platform Fee Tiers (No Code)

Stripe's **Platform Pricing Tool** lets the Toolbox define fee schedules from the Dashboard — no code changes, no redeployment:

```
Default pricing:           15% application fee on all tool calls
Pricing group "scale":     10% for providers with >10k calls/month
Pricing group "enterprise": 8% for contracted enterprise providers
Pricing group "community":  0% for open-source / free-tier tools
```

Volume tiers apply automatically based on transaction properties. The Dolt ledger makes fee structures transparent and auditable — anyone can clone the registry and verify what fees were charged on what calls.

### Stripe Connect (Account Setup)

**Provider onboarding:**

```
1. Provider signs up with Toolbox
2. Toolbox creates a Stripe Account v2 with `merchant` configuration
3. Provider completes Stripe onboarding (identity, bank account)
4. Provider lists their tool in the Dolt registry
5. Tool record's payment field references their Account ID
```

**Operator onboarding:**

```
1. Operator signs up with Toolbox
2. Toolbox creates a Stripe Account v2 with `customer` configuration
3. Operator adds a payment method and pre-funds credits
4. Operator gets a TOOLBOX_ACCOUNT_ID for their agent config
5. Agent can now discover and call any tool in the registry
```

**If a company is both provider and operator** (common), it's one Account with both configurations. No duplication.

**The payment field on the tool record:**

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

The `platform_fee_pct` is deliberately absent from the tool record. Fees are set by the platform via Stripe's Platform Pricing Tool, not declared by the provider. This means the Toolbox can adjust fee tiers without requiring providers to update their records, and providers can't game the system by declaring lower fees.

Or for free/open-source tools:

```json
"payment": {
    "methods": [
        { "type": "free" }
    ]
}
```

### Payment Verification

Payment verification is different depending on who's doing the verifying:

| Party               | What they verify               | How                                                                           |
| ------------------- | ------------------------------ | ----------------------------------------------------------------------------- |
| **Gateway**         | Operator can pay               | Account has valid payment method + sufficient credit balance + under budget   |
| **Gateway**         | Call succeeded before metering | Tool returned valid response; meter event reported to Stripe                  |
| **Provider**        | They got paid                  | Destination transfers appear in their Stripe dashboard; meter summaries match |
| **Provider**        | Specific call was paid for     | Invocation record in Dolt ledger matches meter event ID and transfer          |
| **Upvote verifier** | Upvote is backed by real usage | `proof.meter_event_id` resolves to a real meter event; ledger commit exists   |

The key insight: **the Dolt ledger and Stripe meters are complementary proof systems.** Stripe handles the money. Dolt handles the receipts. Either party can audit by comparing the two. The gateway can't lie about usage without the discrepancy showing up in the ledger-vs-meter reconciliation.

### Extensible via Lexicons

New payment methods don't require protocol changes. Anyone publishes a new lexicon:

```
com.toolbox.defs#paymentStripeConnect  ← MVP payment method (credits + destination charges)
com.toolbox.defs#paymentFree           ← open source / community tools
com.toolbox.defs#paymentLightning      ← machine-native micropayments (no gateway needed)
com.toolbox.defs#paymentCashu          ← bearer token micropayments (no gateway needed)
io.fedi.defs#paymentFedimint           ← community-defined
xyz.newrail.defs#paymentWhatever       ← anyone can extend
```

Validate on read. If a tool lists a payment method the agent doesn't understand, the agent skips it and picks one it does understand. If it can't pay at all, it moves on to the next matching tool.

### Future Payment Methods

As the ecosystem matures and agent-to-agent transactions become more autonomous, machine-native payment rails become compelling — and they **don't need the gateway at all**:

**Lightning (L402)**: Machine-native micropayments. Server returns HTTP 402 with a Lightning invoice, agent pays, gets a macaroon (auth token), calls the tool. No accounts, no API keys, no billing cycles. The payment preimage is cryptographic proof. Best for: autonomous agents with budgets, sub-cent per-call pricing. **No gateway needed** — the agent pays the provider directly.

**Cashu Ecash**: Prepaid bearer tokens. Agent gets an "allowance" of ecash tokens from a Cashu mint. Each tool call burns a token — offline, instant, no round-trip. Provider redeems tokens with the mint. Best for: high-frequency, low-latency agent workflows. **No gateway needed** — the token IS the payment.

These methods slot in alongside Stripe Connect. A tool can list multiple payment methods:

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

When the agent uses `stripe_connect`, the Toolbox gateway handles it and takes its cut. When the agent uses `l402` or `cashu`, the agent pays the provider directly — the Toolbox is cut out of the payment entirely. **The Toolbox has to compete on value** (discovery, reputation, convenience, dispute resolution) rather than lock-in. That's the healthy dynamic.

The progression from Stripe Credits → L402 → Cashu mirrors a broader decentralization arc: start with the most familiar rails (Stripe), graduate to machine-native rails as agent autonomy increases. The tool record's multi-method payment field means this happens tool-by-tool, not all-at-once — a provider can offer Stripe today and add L402 tomorrow without breaking anything.

### Failure and Refunds

If the tool fails after payment, the Dolt ledger provides a full receipts trail:

1. The **meter event** or **Stripe charge** (auditable by both parties)
2. The **invocation record** in Dolt (input hash, output hash, latency, `schema_valid`)
3. The **upvote** with a low quality score and valid payment proof

For credit-based calls: the gateway simply doesn't report the meter event to Stripe, so no credits are drawn down. The failure is recorded in the Dolt ledger.

For destination charges: the gateway creates a refund with `reverse_transfer=true` and `refund_application_fee=true` — Stripe unwinds the entire flow atomically. Both parties see the refund in their dashboards.

For gray areas (tool returned _something_ but it was wrong), the Dolt ledger gives both parties an auditable record to resolve disputes. Low-quality upvotes with valid payment proofs are strong public signals.

---

## Distributed Reputation

### How It Works

Reputation is not stored on the tool. It's **derived** — a materialized view computed from all the upvote records in the Dolt registry:

```
REPUTATION for acme's fraud-detection-v3:

  = Scan all com.toolbox.tool.upvote records in the Dolt registry
    where subject = "com.toolbox.tool/fraud-detection-v3@acme.com"
    AND proof is valid (payment receipt checks out, invocation hash exists in ledger)

  = Aggregate quality scores, count verified upvotes, compute percentiles

Nobody owns this score.
Nobody can inflate it without paying for real usage.
Anybody can compute it from the Dolt data (clone the registry and run the query).
```

### Anti-Gaming Properties

| Attack                              | Why It Fails                                                                                                                                                          |
| ----------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Fake upvotes** (sybil)            | Proof-of-use required. No valid payment receipt = upvote is unverifiable. Anyone computing reputation filters to verified-only.                                       |
| **Self-upvoting**                   | Provider would have to pay themselves to use their own tool. Payment is real money. Ledger shows `caller_did == provider_did` — trivial to filter out.                |
| **Wash trading** (mutual inflation) | Two companies upvoting each other. Detectable via diversity-of-upvoters weighting. PageRank-style graph analysis on the upvote graph.                                 |
| **Buying upvotes**                  | Paying others to use your tool for the upvote. This actually requires real usage and real payment — the tool still has to deliver quality results to get high scores. |
| **Deleting bad reviews**            | Impossible. Upvotes live in the reviewer's repo, not the provider's. The provider cannot touch them.                                                                  |

### Discovery Algorithms

Because the registry is a Dolt database that anyone can clone, anyone can build **discovery algorithms** over the tool and upvote data:

- **"Trending Tools"** — tools with the most upvotes this week
- **"Most Reliable"** — tools with highest SLA compliance in upvote records
- **"My Network Trusts"** — tools upvoted by companies you trust
- **"Best for Financial Analysis"** — filtered by `context.task_type`
- **"Budget Picks"** — highest quality-to-price ratio

These are query endpoints that return ranked lists of tools. Anyone can run one — clone the Dolt registry, write your own ranking SQL, expose it as an API. Competition between discovery algorithms improves quality for everyone.

---

## The Dolt Backbone

### Registry Schema

```sql
-- Accounts (unified identity via Stripe Accounts v2)
-- One account = one identity, whether provider, operator, or both
CREATE TABLE accounts (
    id VARCHAR(255) PRIMARY KEY,            -- acct_abc123 (Stripe Account v2 ID)
    domain VARCHAR(255) NOT NULL,           -- acme.com (verified via DNS TXT or .well-known)
    did VARCHAR(255),                       -- did:plc:acme-corp (Tier 2, nullable)
    display_name VARCHAR(255),
    is_provider BOOLEAN DEFAULT FALSE,      -- has merchant configuration
    is_operator BOOLEAN DEFAULT FALSE,      -- has customer configuration
    stripe_onboarded BOOLEAN DEFAULT FALSE,
    created_at DATETIME,
    updated_at DATETIME
);

-- Tool definitions (immutable, content-addressed)
-- Keyed by hash of schema + invocation + capabilities + provider identity
-- Append-only: rows are never updated or deleted
CREATE TABLE tool_definitions (
    content_hash VARCHAR(64) PRIMARY KEY,    -- sha256 of (schema + invocation + provider identity)
    provider_account VARCHAR(255) NOT NULL,   -- acct_abc123 (Stripe Account v2, unified identity)
    provider_domain VARCHAR(255) NOT NULL,    -- acme.com (verified via DNS TXT or .well-known)
    provider_did VARCHAR(255),                -- did:plc:acme-corp (Tier 2, nullable)
    schema_json JSON NOT NULL,               -- input/output contract
    invocation_json JSON NOT NULL,           -- protocol, endpoint
    capabilities JSON,                       -- capability tags for search
    created_at DATETIME,
    FOREIGN KEY (provider_account) REFERENCES accounts(id)
);

-- Tool listings (mutable, human-readable metadata)
-- Points to a tool_definition via content_hash
-- Tier 1: domain-verified providers push records directly to Dolt
-- Tier 2: AT Protocol providers get an at_uri linking to their PDS repo
CREATE TABLE tool_listings (
    id VARCHAR(255) PRIMARY KEY,             -- com.toolbox.tool/fraud-detection@acme.com
    definition_hash VARCHAR(64) NOT NULL,     -- points to tool_definitions.content_hash
    provider_account VARCHAR(255) NOT NULL,   -- acct_abc123 (Stripe Account v2, unified identity)
    provider_domain VARCHAR(255) NOT NULL,    -- acme.com (verified via DNS TXT or .well-known)
    provider_did VARCHAR(255),                -- did:plc:acme-corp (Tier 2, nullable)
    at_uri VARCHAR(500),                      -- at://did:plc:acme/com.toolbox.tool/fraud-v3 (Tier 2, nullable)
    name VARCHAR(255) NOT NULL,
    version_label VARCHAR(32),                -- "3.1.0" — cosmetic, for humans (like a Git tag)
    description TEXT,
    pricing_json JSON NOT NULL,              -- model, price, currency, bulk
    payment_json JSON NOT NULL,              -- accepted payment methods (Stripe for MVP)
    sla_json JSON,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (definition_hash) REFERENCES tool_definitions(content_hash),
    FOREIGN KEY (provider_account) REFERENCES accounts(id)
);

-- Upvotes (proof-of-use quality signals)
CREATE TABLE upvotes (
    id VARCHAR(255) PRIMARY KEY,            -- com.toolbox.tool.upvote/5kqw3x@agent-company-xyz.com
    tool_id VARCHAR(255) NOT NULL,          -- what tool was upvoted
    caller_account VARCHAR(255) NOT NULL,   -- acct_xyz789 (Stripe Account v2)
    caller_domain VARCHAR(255) NOT NULL,    -- who upvoted (domain-verified)
    caller_did VARCHAR(255),                -- Tier 2, nullable
    quality_score INT,                      -- 1-5
    latency_met_sla BOOLEAN,
    schema_valid BOOLEAN,
    proof_json JSON NOT NULL,               -- meter event ID, invocation hash, ledger commit
    context_json JSON,                      -- task type, complexity
    created_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id),
    FOREIGN KEY (caller_account) REFERENCES accounts(id)
);

-- Invocation ledger (local to each Toolbox node)
CREATE TABLE invocations (
    id VARCHAR(255) PRIMARY KEY,
    tool_id VARCHAR(255) NOT NULL,
    definition_hash VARCHAR(64) NOT NULL,    -- the exact definition called (immutable pin)
    caller_account VARCHAR(255) NOT NULL,   -- acct_xyz789 (Stripe Account v2)
    caller_domain VARCHAR(255) NOT NULL,
    caller_did VARCHAR(255),                -- Tier 2, nullable
    input_hash VARCHAR(64) NOT NULL,        -- sha256 of input
    output_hash VARCHAR(64),                -- sha256 of output
    payment_method VARCHAR(32),             -- "stripe_credits", "stripe_charge", "free", "l402", etc.
    payment_amount DECIMAL(10,4),
    payment_currency VARCHAR(16),           -- "usd" for MVP
    payment_proof VARCHAR(500),             -- meter event ID, charge ID, preimage, etc.
    latency_ms INT,
    schema_valid BOOLEAN,
    created_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id),
    FOREIGN KEY (caller_account) REFERENCES accounts(id)
);

-- Reputation (computed, cached, recomputed periodically)
CREATE TABLE reputation (
    tool_id VARCHAR(255) PRIMARY KEY,
    total_upvotes INT DEFAULT 0,
    verified_upvotes INT DEFAULT 0,         -- with valid proof-of-use
    avg_quality DECIMAL(3,2),
    sla_compliance_pct DECIMAL(5,2),
    unique_callers INT DEFAULT 0,
    total_invocations INT DEFAULT 0,
    computed_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id)
);
```

### Why Dolt Specifically

Every table above gets **Git-style version control for free**:

- `SELECT * FROM tool_listings AS OF '2026-03-01'` — what tools existed on March 1st?
- `SELECT * FROM dolt_diff('main~5', 'main', 'tool_listings')` — what tools changed in the last 5 commits?
- Branch a tool's schema to test changes, merge when validated
- `dolt clone` the registry for offline agent operation
- `dolt push` to DoltHub for public federation
- Pull requests for schema changes — human review before publishing
- Full audit trail: who registered what, when, and every change since

---

## How It Works End-to-End

### For a Company Listing a Tool

```
1. Company already has their tool running (API, MCP server, whatever)
2. They sign up at toolbox.sh
3. Toolbox creates a Stripe Account v2 with `merchant` config
4. Company completes Stripe onboarding (identity, bank account)
5. They submit a tool record (JSON) to the Dolt registry:
   - What it does (schema)
   - Where it lives (endpoint)
   - How to call it (protocol)
   - What it costs (pricing in USD)
   - Their Account ID (for payment routing)
6. They verify domain ownership (DNS TXT record or .well-known)
7. That's it. No SDK. No middleware. No infrastructure changes.
   If they also want to USE tools, they add `customer` config
   to the same account — one identity for both sides.
```

### For an Agent Using a Tool

```
1. Agent needs fraud detection for a financial analysis task
2. Queries the Dolt-backed registry via toolbox_search:
   "capabilities LIKE '%fraud%' AND pricing.currency = 'usd'
    AND sla.p99_latency_ms < 500
    ORDER BY reputation.avg_quality DESC"
3. Gets back matching tools, ranked by quality and price
4. Validates the schema matches its needs (lexicon validation)
5. Reads the payment field: provider accepts Stripe Connect
6. Gateway checks operator's credit balance (local, fast):
   - $0.005 call → credit drawdown path (no Stripe round-trip)
   - Gateway calls the provider's endpoint
   - On success: reports meter event to Stripe (async)
   - On failure: no meter event, no charge, failure logged
7. Dolt ledger records the invocation (the proof layer)
8. Agent calls toolbox_review with proof-of-use
9. Credits draw down at invoice finalization
10. Stripe transfers provider's share on their payout schedule
```

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

## Business Model

### The Platform Play

The Toolbox is a **Stripe Connect platform for AI tool calls**. The hosted service at toolbox.sh handles discovery, payment, and reputation — and takes a platform fee on every tool call routed through it. Stripe's Accounts v2, Billing Credits, Billing Meters, and Platform Pricing Tool handle almost all the financial plumbing — the Toolbox focuses on the registry, gateway, and ledger.

```
┌─────────────────────────────────────────────────────────┐
│  toolbox.sh (the hosted service)                         │
│                                                          │
│  - Public registry (Dolt, clonable by anyone)            │
│  - Hosted gateway (Stripe Connect, volume-tiered fees)   │
│  - Billing Credits for fast-path micropayments           │
│  - Billing Meters for async usage tracking               │
│  - Platform Pricing Tool for no-code fee management      │
│  - Discovery, reputation, all the materialized views     │
│  - You sign up, get a TOOLBOX_ACCOUNT_ID, done           │
│                                                          │
│  "The easy button"                                       │
└──────────────────────────┬──────────────────────────────┘
                           │
                      dolt clone
                           │
┌──────────────────────────▼──────────────────────────────┐
│  your-company's private Toolbox                          │
│                                                          │
│  - Your own registry (fork of public, or from scratch)   │
│  - Your own gateway (your own Stripe, your fees)         │
│  - Internal tools that never touch the public registry   │
│  - You control everything                                │
└─────────────────────────────────────────────────────────┘
```

Nobody complains that GitHub is "centralized" because Git is open and you can leave. Same energy. The Toolbox is honestly centralized where it provides value, and honestly open where it matters.

### Revenue Streams

**Platform fee (MVP, bread and butter)**: Volume-tiered application fees on every tool call routed through the hosted gateway. Managed via Stripe's Platform Pricing Tool — no code changes to adjust tiers. Default 15%, scaling down to 8% for high-volume providers. Agent operator gets one bill from Toolbox.

**Billing Credits margin (MVP)**: Operators pre-fund credit balances. The Toolbox holds the float between credit purchase and provider payout. At scale, this is meaningful.

**Premium discovery (growth)**: Providers pay for promoted placement in `toolbox_search` results. Transparent and labeled — the Dolt registry makes it auditable.

**Enterprise self-hosted (later)**: "Run your own Toolbox" with SLA, support, and managed updates. Like GitLab Enterprise or Confluent for Kafka.

### Value Props

**For a tool provider**: "List your tool once, get discovered by every agent. We handle billing. You get a Stripe deposit. One Account ID, whether you're selling or buying."

**For an agent operator**: "One config line, access to every tool. Pre-fund credits, set a budget, done. One bill from Toolbox."

**For an enterprise**: "Start with our hosted version. When you need control, clone everything and run it yourself. Nothing is trapped."

### Self-Hosted

An enterprise that outgrows the hosted platform — or just wants control — runs their own:

```
1. Clone the public registry: dolt clone toolbox/registry
2. Add internal tools (never published publicly)
3. Run your own gateway (your own Stripe Connect, or no Stripe at all)
4. Point agents at your gateway
5. Pull from the public registry for third-party tools
6. Push upvotes back to the public registry (or don't)
```

For internal tools, there's no payment needed — just auth. For external tools, the enterprise negotiates directly with providers or routes through the public gateway. The software is open, the data is open, the hosted service is the product.

### Why Customers Stay on Hosted

Even though they _could_ leave:

- The providers are already onboarded (network effect)
- The reputation data already exists (cold start problem solved)
- Stripe Connect, Billing Credits, Meters, tax, invoicing — handled
- Gateway infrastructure — no ops burden
- Credit balance management, budget controls — built in
- Platform Pricing Tool — fee tiers without engineering work
- Reputation views, trending, discovery algorithms — computed for you
- Leaving means rebuilding all of that yourself

### The Competitive Moat Over Time

```
Stage 1 (MVP):     Stripe Connect platform fee — we're the billing layer
                   Credit balances solve the latency problem
Stage 2 (Growth):  Network effects — providers and agents concentrate here
                   Billing Credits float generates margin
Stage 3 (Mature):  L402/Cashu bypass Stripe — we compete on discovery + reputation
                   Registry data + reputation graph are the real assets
Stage 4 (Scale):   The registry IS the moat — like npm, whoever has the packages wins
                   Gateway becomes optional; the data layer is everything
```

The key tension: as L402/Cashu mature, agents can pay providers directly and cut the Toolbox out of the payment loop entirely. This is by design. The Toolbox must always earn its place by providing value — discovery, reputation, convenience — not by being a required intermediary. If the only reason people use the hosted service is because they can't pay without it, the platform is fragile. If they use it because it's genuinely the easiest and best way to find and use tools, it's durable.

---

## Adoption Tiers

The Toolbox has a two-tier adoption model. Companies start with the simplest possible on-ramp and upgrade when the value is proven.

### Tier 1: Dolt Registry + Hosted Gateway (MVP)

The lowest-friction path. A company onboards with Toolbox (Stripe Connected Account), submits a JSON tool record to the Dolt registry — via CLI, API, or a PR to a DoltHub repo — and they're discoverable. Identity is domain-verified the old-fashioned way: DNS TXT record or `.well-known/toolbox.json`.

- **Identity**: Domain ownership (e.g., `acme.com`)
- **Payment**: Stripe Connect via the Toolbox hosted gateway (USD)
- **Discovery**: SQL queries against the Dolt registry
- **Reputation**: Proof-of-use upvotes in the Dolt registry
- **Requires**: A JSON file, a domain you own, and a Stripe account. That's it.

### Tier 2: AT Protocol Integration (Upgrade)

When a company wants portable identity, richer federation, and the full distributed data model, they upgrade to a DID and optionally publish records to a PDS. Their domain-verified identity transfers cleanly — AT Protocol already uses domain handles.

- **Identity**: DID anchored to domain (e.g., `did:plc:acme-corp` → `@acme.com`)
- **Payment**: Stripe Connect + future machine-native methods (Lightning, Cashu)
- **Discovery**: Toolbox feeds + cross-network queries via `at://` URIs
- **Reputation**: Upvotes in caller's own repo — portable across registries

The migration path:

```
1. acme.com verified via DNS TXT record (Tier 1)
2. acme.com creates a DID, sets handle to @acme.com (Tier 2)
3. Toolbox links the existing Dolt record to the new at:// URI
4. All existing invocation history and reputation carries over
5. Richer federation and portability unlocked
```

The Dolt backbone makes this seamless — both tiers write to the same tables. A Tier 1 tool and a Tier 2 tool sit side by side in the `tools` table. The only difference is whether the `at_uri` and `provider_did` columns are populated. Agents querying the registry don't care — they see the same schema, same pricing, same endpoint.

---

## What This Is Not

- **Not a runtime**: Tools run on the provider's infrastructure. The Toolbox doesn't execute anything.
- **Not an MCP replacement**: MCP, REST, gRPC are invocation protocols. The Toolbox is discovery, payment, and reputation. They're complementary — invocation protocol is just a field in the tool record.
- **Not a blockchain**: Dolt is a database with Git semantics. It's versioned, auditable, and tamper-evident, but it's not a distributed consensus system. Federation happens via Dolt remotes (like Git remotes), not consensus.
- **Not a Bluesky app**: The Toolbox uses AT Protocol's design patterns (records, lexicons, DIDs) but does not depend on Bluesky's infrastructure, social graph, or relay network. Interoperability with Bluesky is possible but not a goal.
- **Not inescapably centralized**: The hosted service at toolbox.sh is honestly centralized — it's a Stripe Connect platform that takes a cut. But the registry is a Dolt database anyone can clone. The gateway is open source anyone can run. Anyone can stand up their own Toolbox instance, compute their own reputation, or build their own discovery algorithm. The centralization provides value; the exit is real.

---

## The Analogies

| Existing System   | Toolbox Equivalent                                                        |
| ----------------- | ------------------------------------------------------------------------- |
| DNS               | Tool discovery — resolve a capability to an endpoint                      |
| TLS certificates  | Domain verification / DIDs — verify you're talking to who you think       |
| npm registry      | Tool registry — search, install, version                                  |
| Unison hashes     | Content-addressed tool definitions — no version conflicts by construction |
| App Store ratings | Reputation — but only from verified purchasers                            |
| Stripe Connect    | Payment — platform handles billing, provider gets deposited directly      |
| Shopify           | Honestly centralized — you could build your own store, but why would you? |
| Google PageRank   | Discovery algorithms — anyone can rank tools differently                  |
| Git + GitHub      | Dolt + DoltHub — version control for the registry and ledger              |

Or more concisely: **npm + Stripe Connect + Dolt, for AI agent tool calls, with AT Protocol's data philosophy.**

---

## Open Questions

- **Lexicon governance**: Who defines `com.toolbox.*` lexicons? A foundation? A GitHub org? Follow AT Protocol norms — publish early, evolve carefully, let the ecosystem fork if needed.
- **Relay economics**: Who runs the relays that index tool records and upvotes? Likely the same model as AT Protocol relays — some public, some private, some subsidized by tool providers who want discovery.
- **Settlement cadence**: When operators pre-fund credits, the platform holds the float until provider payout. Daily? Weekly? Real-time? Provider trust correlates with payout speed. The Dolt ledger makes any cadence auditable.
- **Dispute resolution beyond automated refunds**: Clear-cut failures (errors, timeouts, schema violations) trigger automatic refunds. But what about gray areas — tool returned _something_ but it was wrong? The Dolt ledger + Stripe meter reconciliation gives both parties auditable evidence. Formal arbitration is still an open design problem.

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
- **Stripe Agent Toolkit**: https://github.com/stripe/ai
- **Stripe Accounts v2 API**: https://docs.stripe.com/connect/accounts-v2
- **Stripe Billing Credits**: https://docs.stripe.com/billing/subscriptions/usage-based/billing-credits
- **Stripe Billing Meters**: https://docs.stripe.com/billing/subscriptions/usage-based/recording-usage
- **Stripe Platform Pricing Tool**: https://docs.stripe.com/connect/platform-pricing-tools
- **Stripe Destination Charges**: https://docs.stripe.com/connect/destination-charges
- **Stripe MCP Server**: https://mcp.stripe.com
