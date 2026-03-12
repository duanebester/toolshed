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

---

## The On-Ramp

Today, developers give agents tools by adding MCP servers and API keys to a JSON config file. The Toolbox doesn't change that pattern — it _is_ that pattern. The Toolbox is an MCP server. You add it to your agent's config exactly like any other tool:

```json
{
  "mcpServers": {
    "toolbox": {
      "command": "npx",
      "args": ["@agent-toolbox/mcp-server"],
      "env": { "STRIPE_CUSTOMER_ID": "cus_abc123" }
    }
  }
}
```

One config entry. Now your agent has access to every tool in the registry. No new paradigm, no behavioral shift — just another MCP server that happens to be a gateway to thousands of tools.

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
│  LAYER 2: GATEWAY (thin routing + auth + metering)         │
│                                                            │
│  "Invoke the tool, handle payment, verify response"        │
│  - Protocol translation (MCP, REST, gRPC — doesn't care)  │
│  - Payment negotiation (reads payment methods from record) │
│  - Usage metering (Stripe metered billing)                 │
│  - Response validation against schema                      │
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

A company registers a tool by submitting a JSON record to the Dolt registry. In the simplest case (Tier 1 — see [Adoption Tiers](#adoption-tiers)), this is as easy as a CLI command or a PR to a DoltHub repo. No PDS required.

```json
// Provider: acme.com (domain-verified)
// Collection: com.toolbox.tool/
// Record key: fraud-detection-v3

{
  "name": "Fraud Detection",
  "version": "3.1.0",
  "description": "Real-time transaction fraud scoring with ML",

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

  "pricing": {
    "model": "per_call",
    "price": 0.005,
    "currency": "usd",
    "bulk": { "1000": 0.004, "10000": 0.0025 }
  },

  "payment": {
    "stripe": {
      "payment_link": "https://buy.stripe.com/acme_tool_fraud",
      "meter_id": "mtr_abc123"
    }
  },

  "sla": {
    "p99_latency_ms": 500,
    "uptime": "99.9%",
    "rate_limit": "1000/min"
  },

  "capabilities": ["fraud", "ml", "financial", "real-time"],

  "createdAt": "2026-03-01T00:00:00Z"
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
    "payment_method": "stripe",
    "stripe_invoice_id": "in_1abc123def456",
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

| Record              | Where It Lives                               | Purpose                                               |
| ------------------- | -------------------------------------------- | ----------------------------------------------------- |
| Tool registration   | Dolt registry (`com.toolbox.tool/`)          | "Here's my tool, schema, pricing, and how to pay"     |
| Tool schema/lexicon | Dolt registry (`com.toolbox.lexicon/`)       | Machine-readable input/output contract                |
| Invocation log      | Dolt ledger (`com.toolbox.tool.invocation/`) | Record of each call (input hash, output hash, timing) |
| Upvote              | Dolt registry (`com.toolbox.tool.upvote/`)   | Quality signal with proof-of-use                      |
| Payment proof       | Field on the upvote record                   | Receipt that payment occurred                         |

---

## Payment

### Philosophy

Payment is not a special subsystem. It's **just a field on the tool record** — the provider declares "send cash this way" as part of their tool registration. The agent reads the payment methods, picks one it supports, pays, and calls the tool. No special payment servers. No payment middleware. The provider says how they want to be paid; the agent pays them directly.

### Payment Methods (MVP)

The MVP uses **Stripe metered billing** with USD pricing. This is the pragmatic choice: enterprises already have Stripe accounts, agents can interact with Stripe's API programmatically, and it handles invoicing, reconciliation, and tax compliance out of the box.

```json
"payment": {
    "stripe": {
        "payment_link": "https://buy.stripe.com/acme_tool_fraud",
        "meter_id": "mtr_abc123"
    }
}
```

Or for free/open-source tools:

```json
"payment": {
    "free": {}
}
```

### Extensible via Lexicons

New payment methods don't require protocol changes. Anyone publishes a new lexicon:

```
com.toolbox.defs#paymentStripe       ← MVP payment method
com.toolbox.defs#paymentFree         ← open source / community tools
com.toolbox.defs#paymentLightning    ← future: machine-native micropayments
com.toolbox.defs#paymentCashu        ← future: bearer token micropayments
io.fedi.defs#paymentFedimint         ← community-defined
xyz.newrail.defs#paymentWhatever     ← anyone can extend
```

Validate on read. If a tool lists a payment method the agent doesn't understand, the agent skips it and picks one it does understand. If it can't pay at all, it moves on to the next matching tool.

### Payment Options in Detail

**Stripe (MVP)**: Metered billing in USD. The Toolbox stores the Stripe meter ID in the tool record. The gateway reports usage events to Stripe's metering API on each invocation. Providers get paid through Stripe's existing settlement infrastructure. Agents (or their operators) connect via Stripe customer accounts. Invoices, purchase orders, and tax compliance come for free.

**Dolt Ledger (Internal)**: For trusted/enterprise environments. Every tool call creates a row in a Dolt table. Companies reconcile monthly. The ledger is versioned, diffable, and tamper-evident.

**Free / Open Source**: Community tools. The `"free"` payment method signals no payment required.

### Future Payment Methods

As the ecosystem matures and agent-to-agent transactions become more autonomous, machine-native payment rails become compelling:

**Lightning (L402)**: Machine-native micropayments. Server returns HTTP 402 with a Lightning invoice, agent pays, gets a macaroon (auth token), calls the tool. No accounts, no API keys, no billing cycles. The payment preimage is cryptographic proof. Best for: autonomous agents with budgets, sub-cent per-call pricing.

**Cashu Ecash**: Prepaid bearer tokens. Agent gets an "allowance" of ecash tokens from a Cashu mint. Each tool call burns a token — offline, instant, no round-trip. Provider redeems tokens with the mint. Best for: high-frequency, low-latency agent workflows where Stripe's round-trip is too slow.

These methods slot in alongside Stripe — providers list multiple payment options, agents pick whichever they support. The architecture doesn't change; only the `payment` field on the tool record grows.

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
-- Tool registrations
-- Tier 1: domain-verified providers push records directly to Dolt
-- Tier 2: AT Protocol providers get an at_uri linking to their PDS repo
CREATE TABLE tools (
    id VARCHAR(255) PRIMARY KEY,            -- com.toolbox.tool/fraud-detection-v3@acme.com
    provider_domain VARCHAR(255) NOT NULL,   -- acme.com (verified via DNS TXT or .well-known)
    provider_did VARCHAR(255),               -- did:plc:acme-corp (Tier 2, nullable)
    at_uri VARCHAR(500),                     -- at://did:plc:acme/com.toolbox.tool/fraud-v3 (Tier 2, nullable)
    name VARCHAR(255) NOT NULL,
    version VARCHAR(32) NOT NULL,
    description TEXT,
    schema_json JSON NOT NULL,              -- input/output contract
    invocation_json JSON NOT NULL,          -- protocol, endpoint
    pricing_json JSON NOT NULL,             -- model, price, currency, bulk
    payment_json JSON NOT NULL,             -- accepted payment methods (Stripe for MVP)
    sla_json JSON,
    capabilities JSON,                      -- capability tags for search
    created_at DATETIME,
    updated_at DATETIME
);

-- Upvotes (proof-of-use quality signals)
CREATE TABLE upvotes (
    id VARCHAR(255) PRIMARY KEY,            -- com.toolbox.tool.upvote/5kqw3x@agent-company-xyz.com
    tool_id VARCHAR(255) NOT NULL,          -- what tool was upvoted
    caller_domain VARCHAR(255) NOT NULL,    -- who upvoted (domain-verified)
    caller_did VARCHAR(255),                -- Tier 2, nullable
    quality_score INT,                      -- 1-5
    latency_met_sla BOOLEAN,
    schema_valid BOOLEAN,
    proof_json JSON NOT NULL,               -- Stripe invoice ID, invocation hash, ledger commit
    context_json JSON,                      -- task type, complexity
    created_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tools(id)
);

-- Invocation ledger (local to each Toolbox node)
CREATE TABLE invocations (
    id VARCHAR(255) PRIMARY KEY,
    tool_id VARCHAR(255) NOT NULL,
    caller_domain VARCHAR(255) NOT NULL,
    caller_did VARCHAR(255),                -- Tier 2, nullable
    input_hash VARCHAR(64) NOT NULL,        -- sha256 of input
    output_hash VARCHAR(64),                -- sha256 of output
    payment_method VARCHAR(32),             -- "stripe", "free", "lightning", etc.
    payment_amount DECIMAL(10,4),
    payment_currency VARCHAR(16),           -- "usd" for MVP
    payment_proof VARCHAR(500),             -- Stripe invoice ID, preimage, tx hash, etc.
    latency_ms INT,
    schema_valid BOOLEAN,
    created_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tools(id)
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
    FOREIGN KEY (tool_id) REFERENCES tools(id)
);
```

### Why Dolt Specifically

Every table above gets **Git-style version control for free**:

- `SELECT * FROM tools AS OF '2026-03-01'` — what tools existed on March 1st?
- `SELECT * FROM dolt_diff('main~5', 'main', 'tools')` — what tools changed in the last 5 commits?
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
2. They submit a tool record (JSON) to the Dolt registry:
   - What it does (schema)
   - Where it lives (endpoint)
   - How to call it (protocol)
   - What it costs (pricing in USD)
   - How to pay (Stripe meter ID)
3. They verify domain ownership (DNS TXT record or .well-known)
4. That's it. No SDK. No middleware. No infrastructure changes.
```

### For an Agent Using a Tool

```
1. Agent needs fraud detection for a financial analysis task
2. Queries the Dolt-backed registry:
   "capabilities LIKE '%fraud%' AND pricing.currency = 'usd'
    AND sla.p99_latency_ms < 500
    ORDER BY reputation.avg_quality DESC"
3. Gets back matching tools, ranked by quality and price
4. Validates the schema matches its needs (lexicon validation)
5. Reads the payment field: provider accepts Stripe metered billing
6. Agent calls the tool at the provider's endpoint (MCP/REST/whatever)
7. Gateway reports usage to Stripe's metering API
8. Gets the result, validates against schema
9. Creates an invocation record in the Dolt ledger
10. Creates an upvote record in the registry (with proof-of-use)
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

## Adoption Tiers

The Toolbox has a two-tier adoption model. Companies start with the simplest possible on-ramp and upgrade when the value is proven.

### Tier 1: Dolt Registry (MVP)

The lowest-friction path. A company submits a JSON tool record directly to the Dolt registry — via CLI, API, or a PR to a DoltHub repo. Identity is domain-verified the old-fashioned way: DNS TXT record or `.well-known/toolbox.json`.

- **Identity**: Domain ownership (e.g., `acme.com`)
- **Payment**: Stripe metered billing (USD)
- **Discovery**: SQL queries against the Dolt registry
- **Reputation**: Proof-of-use upvotes in the Dolt registry
- **Requires**: A JSON file and a domain you own. That's it.

### Tier 2: AT Protocol Integration (Upgrade)

When a company wants portable identity, richer federation, and the full distributed data model, they upgrade to a DID and optionally publish records to a PDS. Their domain-verified identity transfers cleanly — AT Protocol already uses domain handles.

- **Identity**: DID anchored to domain (e.g., `did:plc:acme-corp` → `@acme.com`)
- **Payment**: Stripe + future machine-native methods (Lightning, Cashu)
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
- **Not centralized**: The registry is a Dolt database anyone can clone. Anyone can run their own registry node, compute reputation, or build a discovery algorithm.

---

## The Analogies

| Existing System   | Toolbox Equivalent                                                  |
| ----------------- | ------------------------------------------------------------------- |
| DNS               | Tool discovery — resolve a capability to an endpoint                |
| TLS certificates  | Domain verification / DIDs — verify you're talking to who you think |
| npm registry      | Tool registry — search, install, version                            |
| App Store ratings | Reputation — but only from verified purchasers                      |
| Stripe Connect    | Payment — metered billing, the provider declares how to pay         |
| Google PageRank   | Discovery algorithms — anyone can rank tools differently            |
| Git + GitHub      | Dolt + DoltHub — version control for the registry and ledger        |

Or more concisely: **npm + Stripe + Dolt, for AI agent tool calls, with AT Protocol's data philosophy.**

---

## Open Questions

- **Lexicon governance**: Who defines `com.toolbox.*` lexicons? A foundation? A GitHub org? Follow AT Protocol norms — publish early, evolve carefully, let the ecosystem fork if needed.
- **Relay economics**: Who runs the relays that index tool records and upvotes? Likely the same model as AT Protocol relays — some public, some private, some subsidized by tool providers who want discovery.
- **Agent wallets**: Not as hard as it sounds. V1 is just a Stripe customer ID with a spending cap — the same pattern as OpenAI's API billing. V2 adds per-agent budgets enforced at the gateway. V3 introduces prepaid balances (Stripe customer balance or Cashu tokens). V4 is autonomous agents with their own funds (Lightning, Cashu). The Toolbox architecture doesn't change across these stages — only what's behind the `STRIPE_CUSTOMER_ID` env var.
- **Schema evolution**: When a tool's input/output changes, how do we handle backward compatibility? Follow lexicon rules — additive changes only, breaking changes get a new lexicon name.
- **Privacy**: Invocation logs contain sensitive data (what tools an agent used, for what task). The Dolt ledger could be local-only, with only upvotes (which are intentionally public) going to the shared registry.
- **Dispute resolution**: What if a tool takes payment but returns garbage? The proof-of-use in upvotes creates a public record. Downvotes (low quality scores) with valid payment proofs are strong signals. But formal dispute resolution is an open design problem.

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
- **L402 (HTTP 402 + Lightning)**: https://docs.lightning.engineering/the-lightning-network/l402
- **Cashu (ecash)**: https://cashu.space
- **Bluesky Lexicons**: https://atproto.com/guides/lexicon
- **pdsls (AT Protocol file browser)**: https://pdsls.dev
