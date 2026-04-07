# The Agent Toolshed (v2)

**A free, open, decentralized registry where companies list tools, agents discover them, and reputation emerges from verified usage — backed by Dolt DB, accessed via SSH, inspired by AT Protocol's data philosophy.**

---

## The Problem

AI agents are increasingly capable, but when they need specialized tools — fraud detection, geospatial analysis, compliance checking, sentiment analysis — they hit a wall:

- **No discovery**: Tools are hardcoded or manually configured. There's no Yellow Pages for agent capabilities.
- **No reputation**: An agent can't know which tool provider is reliable, fast, or accurate without a human pre-vetting everything.
- **No portability**: Switch your MCP server or API provider and you're rewiring everything.
- **No audit trail**: When an agent makes a decision based on a tool's output, there's no versioned, reproducible record of what happened.
- **No machine-native interface**: Agents interact with the world through HTTP clients, SDK wrappers, and credential managers. Discovery should be as simple as running a command.

The protocol debate (MCP vs. skills vs. raw REST vs. gRPC) is a distraction. The real gap is: **how do agents find, trust, and audit tool usage across organizational boundaries?**

---

## What Changed (v1 → v2)

The original ToolShed design put a **gateway proxy** in the middle of every tool call. The agent called ToolShed, ToolShed called the provider, ToolShed logged the result. This made ToolShed a bottleneck, a single point of failure, and a holder of everyone's data.

v2 changes the fundamental model:

| Aspect           | v1 (Proxy)                                     | v2 (Discovery)                                              |
| ---------------- | ---------------------------------------------- | ----------------------------------------------------------- |
| **Role**         | Middleman — routes every call                  | Registry — tells you where to go                            |
| **Tool calls**   | Agent → ToolShed → Provider → ToolShed → Agent | Agent → Provider (direct)                                   |
| **Identity**     | Stripe Account IDs, API keys                   | SSH public keys (zero signup)                               |
| **Interface**    | HTTP API + MCP server                          | SSH commands + YAML                                         |
| **Payment**      | Stripe Connect platform (required)             | Declared on tool record (settled between parties)           |
| **Proof of use** | Gateway observed the call                      | Agent self-reports, provider counter-attests                |
| **Data seen**    | All inputs and outputs flow through gateway    | Only hashes and metadata                                    |
| **Failure mode** | ToolShed down = all tools down                 | ToolShed down = no new discovery, existing tools still work |

**ToolShed is DNS, not a CDN.** DNS tells you where to go. It doesn't proxy your traffic. ToolShed tells agents what tools exist and where they live. The agent calls the tool directly.

---

## Inspirations

| Source                                                                             | What We Take                                                                                                                        | Key Insight                                                                                                                                                                         |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [AT Protocol / "A Social Filesystem"](https://overreacted.io/a-social-filesystem/) | Records, "data outlives software" philosophy, `/.well-known` discovery. **Not** Bluesky's relay/firehose or social graph.           | _"Our memories, our thoughts, our designs should outlive the software we used to create them."_ Replace "software" with "agent frameworks" and the same principle applies to tools. |
| [terminal.shop](https://terminal.shop)                                             | SSH as the primary interface. Public key identity. Interactive TUI + non-interactive commands over the same protocol.               | SSH gives you identity for free (keys), encryption for free (protocol), and universality (every machine has a client). No API keys, no OAuth, no browser.                           |
| [Gas Town (Steve Yegge)](https://github.com/steveyegge/gastown)                    | Dolt as the backbone for all agent state. Every mutation is a commit. Federation via Dolt remotes.                                  | Dolt gives agents **reproducibility, auditability, and collaboration** on structured data, using branch/merge/diff workflows developers already know from Git.                      |
| [Dolt DB](https://www.dolthub.com/blog/2024-10-15-dolt-use-cases/)                 | Git-for-data: `AS OF` queries, `dolt_diff()`, branch/merge, `dolt clone`/`dolt push`, DoltHub for public hosting and PRs.           | A SQL database with full version history — time-travel, diffing, forking, and federation come free.                                                                                 |
| [Unison](https://www.unison-lang.org/docs/the-big-idea/)                           | Content-addressed identity for tool definitions. Two-layer split: immutable definitions (keyed by hash) and mutable names/metadata. | A "new version" of a tool isn't a mutation — it's a new hash. No version conflicts by construction.                                                                                 |
| [Charm libraries](https://charm.sh)                                                | `wish` (SSH server), `bubbletea` (TUI framework), `lipgloss` (styling), `bubbles` (components). All Go.                             | The entire SSH + TUI stack is Go-native, composing cleanly with the existing ToolShed Go codebase.                                                                                  |

---

## The On-Ramp

### For Agents: SSH

An agent discovers and interacts with tools via SSH. No SDK. No HTTP client library. No credential rotation. Just SSH — which is available in every runtime, every container, every agent sandbox.

```bash
# Search the registry
ssh toolshed.sh search "fraud detection"

# Get tool details
ssh toolshed.sh info acme.com/fraud-detection

# Crawl a domain's tools
ssh toolshed.sh crawl acme.com

# Report a tool call (for reputation)
ssh toolshed.sh report --tool acme.com/fraud-detection \
  --latency 45ms --success --input-hash sha256:abc

# Upvote a tool
ssh toolshed.sh upvote acme.com/fraud-detection --quality 4 --useful

# Verify domain ownership
ssh toolshed.sh verify acme.com

# Interactive TUI browser
ssh toolshed.sh
```

**Identity is built-in.** The first time you connect, your SSH public key fingerprint becomes your account ID. Zero signup friction. No email, no password, no OAuth.

### For Providers: A YAML File

Providers declare their tools in a succinct YAML format — human-writable, agent-parseable:

```yaml
# /.well-known/toolshed.yaml
version: "0.1"
provider:
  domain: acme.com
  contact: tools@acme.com

tools:
  - name: Fraud Detection
    description: Real-time fraud scoring for transactions
    version: "1.0.0"
    capabilities: [fraud-detection, real-time, fintech]
    invoke:
      protocol: rest
      endpoint: https://api.acme.com/fraud
      tool_name: fraud_detection
    schema:
      input:
        transaction_id: { type: string }
        amount: { type: number, min: 0 }
        merchant_category: { type: string }
      output:
        risk_score: { type: number, min: 0, max: 100 }
        flags: { type: array, items: { type: string } }
    pricing:
      model: free
```

~25 lines for a complete tool registration. An agent can read it. A human can write it. The registry computes the content hash server-side.

Providers host `/.well-known/toolshed.yaml` on their domain, then trigger indexing: `ssh toolshed.sh crawl acme.com` (or anyone can trigger it — the registry fetches and verifies the YAML from the domain directly).

### For MCP Users: An MCP Server (Still Works)

The MCP server from v1 still exists. It wraps the SSH/HTTP search API into MCP tools that Claude, Cursor, etc. can use:

```json
{
  "mcpServers": {
    "toolshed": {
      "command": "npx",
      "args": ["@agent-toolshed/mcp-server"]
    }
  }
}
```

No account ID needed. The MCP server calls the registry for discovery. The agent calls tools directly. ToolShed is never in the call path.

### The Meta-Tools

The ToolShed exposes a small set of meta-tools (via SSH, HTTP, or MCP):

- **`search`** — find tools by capability, price, reputation
- **`info`** — get full details on a specific tool
- **`crawl`** — index tools from a domain's `/.well-known/toolshed.yaml`
- **`report`** — submit an invocation report (for reputation)
- **`upvote`** — submit a quality review (linked to a report)
- **`verify`** — verify domain ownership
- **`help`** — structured YAML command catalog (agent self-discovery)

### The Flow

```
1. Agent has a task: "analyze this transaction for fraud"
2. Agent doesn't have a fraud tool → ssh toolshed.sh search "fraud detection"
3. ToolShed returns YAML results from the Dolt registry
4. Agent picks one, reads the endpoint and schema
5. Agent calls the tool DIRECTLY: POST https://api.acme.com/fraud { ... }
6. Agent gets the result, uses it
7. Optionally: ssh toolshed.sh report --tool acme.com/fraud --latency 45ms --success
8. Optionally: ssh toolshed.sh upvote acme.com/fraud --quality 5 --useful
```

ToolShed is involved in steps 2-3 (discovery) and 7-8 (reputation). It is **never** in the call path (step 5).

---

## Architecture

### The Two-Layer Split

```
┌────────────────────────────────────────────────────────────┐
│  LAYER 1: REGISTRY (Dolt-backed, shared, clonable)         │
│                                                            │
│  "What exists, who provides it, what's the contract"       │
│  - Tool records (schema, endpoint, capabilities, pricing)  │
│  - Identity (SSH key fingerprints, domain verification)    │
│  - Upvotes (proof-of-use quality signals)                  │
│  - Reputation (materialized from upvotes + reports)        │
│  - Clonable by anyone: dolt clone toolshed/registry        │
└───────────────────────┬────────────────────────────────────┘
                        │
┌───────────────────────▼────────────────────────────────────┐
│  LAYER 2: LEDGER (Dolt — the audit trail, local)           │
│                                                            │
│  "Who called what, when, what happened"                    │
│  - Every report = a Dolt commit                            │
│  - Input hash, output hash, timing, success/failure        │
│  - Tied to SSH key fingerprint (identity)                  │
│  - Local to each node — never shared or cloned             │
│  - The proof layer for upvotes                             │
└────────────────────────────────────────────────────────────┘
```

There is no Layer 2 "gateway" proxy anymore. The SSH server IS the registry service — it handles discovery queries, registration, reporting, and reputation. It never touches tool call traffic.

### Protocol Agnosticism

From the ToolShed's perspective, the invocation method is just a field in the tool record:

```yaml
invoke:
  protocol: rest
  endpoint: https://api.acme.com/fraud
  tool_name: fraud_detection
```

The `protocol` could be `rest`, `mcp`, `grpc`, `graphql`, or anything else. ToolShed doesn't care — it's not routing the call. It's like how DNS doesn't care what protocol you speak once you've resolved the address. The **schema is the contract**; the protocol is a transport detail.

Companies keep running their tools on their own infrastructure. They don't install our software. They don't change their API. They just publish a YAML file that says: _"here's what I've got, here's how to reach it, here's what it costs."_

---

## SSH-First Identity

### The Key Is Your Account

SSH public key authentication solves the hardest bootstrapping problem in any registry: **identity without signup**.

```
First connection:
  1. You run: ssh toolshed.sh
  2. Your SSH client presents your public key
  3. ToolShed computes: SHA256 fingerprint → that's your account ID
  4. Account created automatically. Zero friction.

Subsequent connections:
  1. Same key → same account → same history, reputation, tools
```

No email. No password. No OAuth flow. No API key to rotate. No billing dashboard to navigate. The SSH key you already have is your identity.

### Domain Verification

To link a domain to your SSH key:

```bash
ssh toolshed.sh verify acme.com
# Returns: Add this DNS TXT record: toolshed-verify=sha256:your-key-fingerprint
# Or: Place this at https://acme.com/.well-known/toolshed-verify.txt
```

Once verified, your key is bound to that domain. Tools you crawl are attributed to `acme.com`. Your upvotes carry the weight of a domain-verified identity.

### Why SSH Works for Agents

- **Every runtime has it.** Python's `subprocess.run(["ssh", ...])`. Node's `child_process.exec`. Go's `os/exec`. No SDK needed.
- **Keys are managed by the OS.** No credential files to leak, no env vars to set, no token refresh logic.
- **Works in containers.** Mount a key, done. Works in CI/CD, in sandboxes, in agent runtimes.
- **Non-interactive by default.** `ssh toolshed.sh search "x"` returns structured YAML to stdout. Perfect for piping.
- **Interactive when needed.** `ssh toolshed.sh` (no args) opens a full TUI for browsing.

---

## Everything Is Records

Every entity in the system is a **record** — a structured document with a schema. There are no special servers for reputation or discovery. It's all records in the Dolt registry, with materialized views computed by whoever needs them.

### The Tool Record (YAML)

A company publishes a `toolshed.yaml` file on their domain. The registry crawls it and splits it into two parts: an immutable **definition** (the contract) and a mutable **listing** (the metadata).

The registry hashes the definition's schema, invocation, capabilities, and provider domain to produce a `content_hash` — the tool's true identity (inspired by Unison's content-addressed definitions). Names, pricing, and descriptions are mutable metadata on the listing that point to the hash.

```yaml
# This is what a provider writes:
version: "0.1"
provider:
  domain: acme.com
  contact: tools@acme.com

tools:
  - name: Fraud Detection
    description: Real-time transaction fraud scoring with ML
    version: "3.1.0"
    capabilities: [fraud, ml, financial, real-time]
    invoke:
      protocol: rest
      endpoint: https://api.acme.com/fraud
      tool_name: fraud_detection
    schema:
      input:
        transaction_id: { type: string }
        amount: { type: number, min: 0 }
        merchant_category: { type: string }
      output:
        risk_score: { type: number, min: 0, max: 100 }
        flags: { type: array, items: { type: string } }
    pricing:
      model: per_call
      price: 0.005
      currency: usd
    payment:
      methods:
        - type: stripe_connect
          account_id: acct_acme_abc123
        - type: free_tier
          limit: 100/month
```

The registry parses this, computes the content hash from the immutable fields (schema + invocation + capabilities + provider domain), and stores:

- **Definition** → immutable, keyed by `sha256:a1b2c3...`
- **Listing** → mutable metadata pointing to the definition hash

### Content-Addressed Tools

Inspired by Unison, where every definition is identified by a hash of its content:

- **No breaking changes, ever.** New schema → new hash → new definition. Old hash still exists.
- **No version conflicts.** Two definitions with different schemas are just different hashes.
- **Agents pin by hash, not by name.** After a successful call, an agent stores `sha256:abc123` — immutable, precise.
- **Identical tools deduplicate.** Two providers publishing the same contract share a content hash.
- **Version labels are cosmetic.** `"3.1.0"` is for humans. The hash is the real identity.

### The Report Record

When an agent calls a tool directly, it can report back to ToolShed for the reputation system:

```bash
ssh toolshed.sh report \
  --tool acme.com/fraud-detection \
  --definition-hash sha256:a1b2c3 \
  --latency 45ms \
  --success \
  --input-hash sha256:deadbeef \
  --output-hash sha256:cafebabe
```

This creates a ledger entry tied to the SSH key:

```yaml
# Stored in the ledger (local, per-node)
id: inv_abc123
tool_id: acme.com/fraud-detection
definition_hash: sha256:a1b2c3
key_fingerprint: SHA256:nThbg6kXUpJW...
input_hash: sha256:deadbeef
output_hash: sha256:cafebabe
latency_ms: 45
success: true
created_at: "2026-03-15T14:23:00Z"
```

ToolShed never sees the actual inputs or outputs. Only hashes.

### The Upvote Record

After reporting a call, an agent can upvote:

```bash
ssh toolshed.sh upvote acme.com/fraud-detection --quality 5 --useful
```

The system checks: "has this SSH key reported an invocation of this tool?" If yes, the upvote is linked to that report. If no, the upvote is rejected.

```yaml
# Stored in the registry (shared, clonable)
id: up_xyz789
tool_id: acme.com/fraud-detection
key_fingerprint: SHA256:nThbg6kXUpJW...
invocation_id: inv_abc123
invocation_hash: sha256:...
ledger_commit: dolt:76qerj...
quality: 5
useful: true
created_at: "2026-03-15T14:23:05Z"
```

No payment proof needed. The SSH key + ledger invocation record is the proof. You can't upvote a tool you haven't called.

### Summary of Record Types

| Record          | Where It Lives                               | Purpose                                                               |
| --------------- | -------------------------------------------- | --------------------------------------------------------------------- |
| Tool definition | Dolt registry (`tool_definitions`)           | Immutable contract: schema, invocation, capabilities                  |
| Tool listing    | Dolt registry (`tool_listings`)              | Mutable metadata: name, pricing, description → points to a definition |
| Report          | Dolt ledger (`invocations`) — **local only** | Record of a call: hashes, timing, success                             |
| Upvote          | Dolt registry (`upvotes`)                    | Quality signal with proof-of-use (linked to a report)                 |
| Account         | Dolt registry (`accounts`)                   | SSH key fingerprint, domain, verification status                      |
| Reputation      | Dolt registry (`reputation`) — **computed**  | Materialized view aggregated from upvotes and reports                 |

---

## Payment (Decoupled)

### Philosophy

**ToolShed is not a payment processor.** Payment is a field on the tool record — the provider declares "here's how to pay me" and the agent/operator settles directly with the provider. ToolShed's job is to make tools findable and accountable, not to move money.

This is a deliberate departure from v1, where Stripe Connect was central to the design. Payment integration is still possible (and the tool record supports declaring payment methods), but it's not required. Free tools are first-class citizens. The registry works without any payment infrastructure.

### Payment Methods on the Tool Record

```yaml
pricing:
  model: free # or per_call, subscription, contact

payment:
  methods:
    - type: free
    - type: stripe_connect
      account_id: acct_acme_abc123
    - type: api_key
      signup_url: https://acme.com/developers
    - type: l402
      endpoint: https://api.acme.com/l402/fraud
      price_sats: 50
```

The agent reads the payment methods and decides how to proceed. If it's free, just call. If it's Stripe, the operator handles billing out-of-band. If it's L402, the agent pays per-call with Lightning. ToolShed surfaces this information. It doesn't process any of it.

### Future Payment Integration

When agentic payment systems mature (Stripe's Agent SDK, L402, Cashu, etc.), the tool record already has the extension point. New payment types are just new entries in the `methods` array. The registry schema doesn't change.

The key principle: **ToolShed must compete on value** (discovery, reputation, convenience) rather than being a required payment intermediary.

---

## Distributed Reputation

### How It Works

Reputation is not stored on the tool. It's **derived** — a materialized view computed from upvote records and invocation reports:

```
REPUTATION for acme.com/fraud-detection:

  = Scan all upvotes WHERE tool_id = "acme.com/fraud-detection"
    AND upvote links to a valid invocation report in the ledger

  = Aggregate quality scores, count verified upvotes, compute percentiles

Nobody owns this score.
Nobody can inflate it without actually calling the tool.
Anybody can compute it from the Dolt data (clone the registry and run the query).
```

### SSH Identity Makes Upvotes Simple

The original design required payment proof (Stripe meter events) to validate upvotes. With SSH keys, the proof model is much simpler:

```
OLD (v1 — payment-dependent):
  1. Agent calls tool → payment happens → Stripe meter event created
  2. Agent wants to upvote → must provide meter_event_id as proof
  3. Gateway verifies with Stripe → "yes, this payment happened"
  4. Upvote accepted
  Problem: no payments = no proof = no upvotes

NEW (v2 — identity-dependent):
  1. Agent calls tool directly → reports back with hashes and timing
  2. Agent upvotes: ssh toolshed.sh upvote acme.com/fraud --quality 4
  3. Registry checks ledger: "has this SSH key reported a call to this tool?"
     → Yes, report inv_abc123 exists → upvote accepted and linked
  4. No payment needed. The ledger IS the proof.
```

### Reputation Weighting

Not all upvotes are equal. The system naturally weights by trust signals:

| Signal                                             | Weight | Why                                  |
| -------------------------------------------------- | ------ | ------------------------------------ |
| Domain-verified key                                | High   | Linked to a real organization        |
| Key with 100+ reports across many tools            | High   | Established usage history            |
| Fresh key, only ever called one tool               | Low    | Could be sybil                       |
| Key fingerprint matches the provider               | Zero   | Self-upvote (automatically filtered) |
| Key with diverse tool usage and consistent reviews | High   | Trustworthy reviewer                 |

### Anti-Gaming Properties

| Attack                    | Why It's Hard                                                                                |
| ------------------------- | -------------------------------------------------------------------------------------------- |
| **Sybil (fake accounts)** | New SSH keys have zero history. Upvotes from fresh keys are near-worthless.                  |
| **Self-upvoting**         | Must call your own tool and report it. Key matches provider → filtered. Patterns detectable. |
| **Ballot stuffing**       | Requires actual invocation reports per key. Can't upvote without reporting a call.           |
| **Drive-by upvotes**      | Can't upvote without a linked invocation report. The ledger is the gatekeeper.               |
| **Deleting bad reviews**  | Impossible. Upvotes live in the shared registry. Dolt commits are immutable.                 |

### Discovery Algorithms

Because the registry is a Dolt database anyone can clone, anyone can build **discovery algorithms** over the data — "Trending Tools," "Most Reliable," "Best for Financial Analysis," "Budget Picks." Clone the registry, write your own ranking SQL, expose it as an API. Competition between discovery algorithms improves quality for everyone.

---

## The Dolt Backbone

### Why Dolt

Every table in the registry gets **Git-style version control for free**:

- `SELECT * FROM tool_listings AS OF '2026-03-01'` — what tools existed on March 1st?
- `SELECT * FROM dolt_diff('main~5', 'main', 'tool_listings')` — what changed in the last 5 commits?
- `dolt clone` the registry for offline agent operation
- `dolt push` to DoltHub for public federation
- Pull requests for schema changes — human review before publishing
- Full audit trail: who registered what, when, and every change since

### Two Dolt Databases

**Registry DB (shared, clonable)**

```
schema/registry/

Tables: accounts, tool_definitions, tool_listings, upvotes, reputation
Hosted on DoltHub: toolshed/registry
Anyone can: dolt clone toolshed/registry
```

This is the public catalog. Definitions, listings, upvotes, reputation. No secrets, no call data.

**Ledger DB (local, private, per-node)**

```
schema/ledger/

Tables: invocations
Never shared. Never cloned. Never pushed.
Lives on the ToolShed node's local disk.
```

The audit trail. Every invocation report gets a Dolt commit with the record (hashes, timing, success/failure). Raw call inputs and outputs are NOT stored — only their hashes. This is what makes "we don't store your data" true while giving both parties an auditable receipt.

### Data Locality

| Data                                 | Where                                  | Why                                       |
| ------------------------------------ | -------------------------------------- | ----------------------------------------- |
| Tool catalog (definitions, listings) | **Shared** — Dolt, clonable by anyone  | This is the product. Must be open.        |
| Accounts (identity, domain)          | **Shared** — Dolt                      | Identity must be portable and verifiable. |
| Upvotes (quality signals)            | **Shared** — Dolt                      | Reputation must be computable by anyone.  |
| Invocation reports                   | **Local** — Dolt, private to each node | Call metadata stays private. Only hashes. |
| Call inputs/outputs                  | **Nowhere**                            | ToolShed doesn't store call data. Period. |

---

## The SSH Server

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    toolshed.sh                           │
│                                                         │
│  SSH Server (charmbracelet/wish)                        │
│  ┌───────────────────────────────────────────────────┐  │
│  │  Auth: SSH public key → account lookup/create     │  │
│  └──────────┬────────────────────────────────────────┘  │
│             │                                           │
│  ┌──────────▼──────────┐  ┌──────────────────────────┐  │
│  │  Interactive Mode   │  │  Command Mode            │  │
│  │  (bubbletea TUI)    │  │  (non-interactive)       │  │
│  │                     │  │                          │  │
│  │  • Browse tools     │  │  search "query"          │  │
│  │  • Search + filter  │  │  info domain/tool        │  │
│  │  • Crawl domains    │  │  crawl domain.com        │  │
│  │  • View reputation  │  │  report --tool ...       │  │
│  │  • Verify domain    │  │  verify domain.com       │  │
│  │  • Upvote tools     │  │  upvote domain/tool ...  │  │
│  └──────────┬──────────┘  └───────────┬──────────────┘  │
│             │                         │                 │
│  ┌──────────▼─────────────────────────▼──────────────┐  │
│  │  Shared Service Layer                             │  │
│  │  internal/core + internal/dolt                    │  │
│  │                                                   │  │
│  │  Dolt Registry ←→ Content Hashing ←→ Validation   │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### Tech Stack

All Go. All composable with the existing `internal/` packages.

| Library                   | Purpose                                                    |
| ------------------------- | ---------------------------------------------------------- |
| `charmbracelet/wish`      | SSH server framework — connections, auth, key management   |
| `charmbracelet/bubbletea` | TUI framework (Elm architecture) — rendering, input, state |
| `charmbracelet/lipgloss`  | CSS-like terminal styling — colors, borders, padding       |
| `charmbracelet/bubbles`   | Pre-built TUI components — tables, inputs, spinners, lists |
| `charmbracelet/glamour`   | Markdown rendering in terminal                             |
| `go-sql-driver/mysql`     | Dolt speaks MySQL wire protocol                            |
| `gopkg.in/yaml.v3`        | YAML parsing for toolshed.yaml                             |

The SSH server is the sole entry point to the registry. All commands share the same `internal/` packages — same types, same validation, same Dolt queries.

### Command Mode Output

Non-interactive commands return structured YAML to stdout — agent-friendly by default:

```bash
$ ssh toolshed.sh search "fraud detection"
```

```yaml
results:
  - name: Fraud Detection
    id: acme.com/fraud-detection
    definition_hash: sha256:a1b2c3d4e5f6
    description: Real-time transaction fraud scoring with ML
    capabilities: [fraud, ml, financial, real-time]
    invoke:
      protocol: rest
      endpoint: https://api.acme.com/fraud
      tool_name: fraud_detection
    schema:
      input:
        transaction_id: { type: string }
        amount: { type: number, min: 0 }
        merchant_category: { type: string }
      output:
        risk_score: { type: number, min: 0, max: 100 }
        flags: { type: array, items: { type: string } }
    pricing:
      model: per_call
      price: 0.005
      currency: usd
    reputation:
      avg_quality: 4.7
      verified_upvotes: 1243
      sla_compliance_pct: 99.2
    provider:
      domain: acme.com
      verified: true

  - name: Fraud Shield
    id: shields.io/fraud-shield
    # ...

total: 12
```

An agent reads this YAML, picks a tool, and calls it directly at the endpoint. ToolShed is done.

---

## The Well-Known Convention

### `/.well-known/toolshed.yaml`

Providers host their tool records on their own domain. ToolShed crawls these endpoints and indexes the records into Dolt for search. The data lives on the provider's domain — if ToolShed disappears, the tool records still exist.

```
Provider's domain (acme.com)
  └── /.well-known/toolshed.yaml
      {
        version: "0.1"
        provider:
          domain: acme.com
          ...
        tools:
          - name: Fraud Detection
            ...
      }
```

The transition is smooth:

1. **Today**: the registry crawls provider domains with `ssh -p 2222 localhost crawl acme.com`
2. **Soon**: providers host `/.well-known/toolshed.yaml` AND the registry indexes them automatically
3. **Eventually**: the registry is purely an index — the provider's domain is the source of truth

The agent interface doesn't change. Discovery works the same way regardless of where the data came from.

### Content Hash Verification

When the crawler (`internal/crawl/`) fetches a provider's `toolshed.yaml`, it:

1. Fetches `https://<domain>/.well-known/toolshed.yaml` (30s timeout, 1MB body cap)
2. Parses the YAML through the standard `core.ParseProviderFileFromBytes` pipeline
3. **Security check**: verifies `provider.domain` in the YAML matches the domain being crawled (prevents `evil.com` from claiming to be `acme.com`)
4. Re-computes the content hash from the immutable fields via `core.ContentHash`
5. Indexes valid records into Dolt with `source: "crawl"`

The hash is **derived, not declared by the provider**. This prevents tampering and ensures consistency across crawls.

The crawl command is available via both SSH and HTTP:

```bash
ssh -p 2222 localhost crawl acme.com
curl -X POST http://localhost:8080/api/crawl -d '{"domain": "acme.com"}'
```

---

## Business Model

### The Free, Open Default

The registry is free. Search is free. Registration is free. Upvotes are free. The entire discovery and reputation system works without payment.

This is the foundation. Everything else is built on top.

### Revenue Streams (Future)

- **Premium discovery**: Promoted placement in search results. Transparent and labeled.
- **Enterprise self-hosted**: "Run your own ToolShed" with SLA, support, and managed updates.
- **Analytics**: Aggregated usage trends, market intelligence for tool providers.
- **Verified badges**: Enhanced verification for providers (security audits, uptime monitoring).

### What We Don't Do

- **Platform fees on tool calls**: ToolShed is not in the call path. Can't take a cut of something we don't touch.
- **Required payment processing**: Payment is between the agent/operator and the provider. ToolShed surfaces the payment info, doesn't process it.
- **Data monetization**: We don't see call data. We can't sell what we don't have.

### The Moat

```
Stage 1 (MVP):     The registry — seed it with real tools, make discovery work
Stage 2 (Growth):  Network effects — providers and agents concentrate here
Stage 3 (Mature):  Reputation data — the upvote/report graph is the real value
Stage 4 (Scale):   The registry IS the moat — like npm, whoever has the packages wins
```

Nobody complains that GitHub is "centralized" because Git is open and you can leave. Same energy. The registry is a Dolt database anyone can clone. The SSH server is open source anyone can run.

---

## Mental Model

| Existing System     | ToolShed Equivalent                                         |
| ------------------- | ----------------------------------------------------------- |
| DNS                 | Tool discovery — resolve a capability to an endpoint        |
| `/.well-known/`     | Provider self-declaration of tools                          |
| npm registry        | Tool registry — search, discover, version                   |
| npmjs.com           | toolshed.sh — the hosted service, the "easy button"         |
| Verdaccio           | Self-hosted ToolShed — same software, your infrastructure   |
| SSH authorized_keys | Identity — your key is your account                         |
| terminal.shop       | SSH as the primary user interface                           |
| Unison hashes       | Content-addressed tool definitions                          |
| App Store ratings   | Reputation — but only from verified users with proof-of-use |
| Git + GitHub        | Dolt + DoltHub — version control for the registry           |

Or more concisely: **npm + SSH + Dolt, for AI agent tool calls, with AT Protocol's data philosophy and terminal.shop's interface model.**

### What This Is Not

- **Not a proxy**: Tools are called directly by the agent. ToolShed is never in the call path.
- **Not a payment processor**: Payment info is on the tool record. Settlement is between parties.
- **Not a runtime**: Tools run on the provider's infrastructure. ToolShed doesn't execute anything.
- **Not a data store**: ToolShed never sees call inputs or outputs. Only hashes in reports.
- **Not an MCP replacement**: MCP, REST, gRPC are invocation protocols. ToolShed is discovery and reputation.
- **Not a blockchain**: Dolt has Git semantics — versioned and auditable, but not distributed consensus.
- **Not inescapably centralized**: The registry is a Dolt database anyone can clone. The server is open source.

---

## Open Questions

- **Report honesty**: Self-reported invocation data could be fabricated. Mitigations: key reputation weighting, provider counter-attestation, statistical anomaly detection. Is this enough?
- **Provider counter-attestation**: Should providers optionally report invocations too? When both sides agree, that's strong signal. When they don't, that's weak signal. How to implement without adding friction?
- **Agentic payments**: Stripe's Agent SDK, L402, Cashu — which payment systems work well for agent-to-provider settlement? Research needed. The tool record already supports declaring multiple methods.
- **Crawl frequency**: How often should ToolShed re-crawl `/.well-known/toolshed.yaml`? Provider-triggered refresh via `ssh -p 2222 localhost crawl --refresh`? Webhook? Poll? (The one-shot `crawl <domain>` command is implemented — see `internal/crawl/`.)
- **Schema evolution**: When a provider updates their schema (new hash), how do agents discover the migration path? Deprecation notices on old listings?
- **Federation**: Multiple ToolShed instances syncing via Dolt remotes. How do upvotes and reputation compose across instances?
- **Abuse**: SSH key rate limiting, spam tool registration, fraudulent domain verification. What are the attack vectors and mitigations?
- **Search ranking**: With free registration, how to prevent the registry from being flooded with low-quality tools? Reputation-weighted search is part of the answer, but cold-start is hard.

---

## Prior Art and References

- **AT Protocol**: https://atproto.com/guides/overview
- **"A Social Filesystem" (Dan Abramov)**: https://overreacted.io/a-social-filesystem/
- **terminal.shop**: https://terminal.shop — SSH-first commerce
- **Charm libraries**: https://charm.sh — wish, bubbletea, lipgloss, bubbles, glamour
- **Gas Town (Steve Yegge)**: https://github.com/steveyegge/gastown
- **Dolt DB**: https://docs.dolthub.com/introduction/what-is-dolt
- **Dolt Use Cases**: https://www.dolthub.com/blog/2024-10-15-dolt-use-cases/
- **Unison — The Big Idea**: https://www.unison-lang.org/docs/the-big-idea/
- **L402 (HTTP 402 + Lightning)**: https://docs.lightning.engineering/the-lightning-network/l402
- **Cashu (ecash)**: https://cashu.space
- **Stripe Agent SDK**: https://github.com/stripe/ai
- **GitHub MCP Registry**: https://github.com/mcp

---

## Implementation Status

The core prototype loop is complete. An agent can SSH in, search, discover tools, crawl provider domains, report calls, and upvote. All v2 fundamentals work: SSH identity, YAML spec, content hashing, decoupled payments (including MPP), Dolt backbone, two-DB split. Fly.io deployment is ready (Dockerfile + fly.toml + startup script).

### ✅ Built (prototype-complete)

| Feature                                                         | Location                                                                                   |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| SSH-first interface (wish)                                      | `cmd/ssh/`, `internal/ssh/`                                                                |
| YAML spec parsing + full validation                             | `internal/core/yaml.go`, `internal/core/types.go`                                          |
| SSH key = identity (auto-create account on connect)             | `internal/ssh/server.go`                                                                   |
| Content-addressed definitions (SHA-256)                         | `internal/core/hash.go`                                                                    |
| Two-DB split (registry + ledger)                                | `schema/registry/`, `schema/ledger/`, `internal/dolt/`                                     |
| 7 meta-tools: search, info, crawl, report, upvote, verify, help | `internal/ssh/commands.go`                                                                 |
| `help` command (agent self-discovery)                           | `internal/ssh/commands.go` — structured YAML command catalog                               |
| Interactive TUI (bubbletea)                                     | `internal/ssh/tui.go` — search, browse, detail views over SSH                              |
| `.well-known/toolshed.yaml` crawler                             | `internal/crawl/crawl.go` — domain security check, 1MB body cap, content hash verification |
| Protocol agnosticism (rest/mcp/grpc/graphql)                    | Field in the tool record, validated on parse                                               |
| Payment decoupled (free, stripe, api_key, l402, cashu, mpp)     | All in `PaymentMethod` type                                                                |
| Simplified reputation                                           | Derived from upvotes + reports                                                             |
| Dolt Docker Compose                                             | `docker-compose.yml`                                                                       |
| Seed data (fraud-detection + word-count examples)               | `schema/registry/seed.sql`                                                                 |
| Word count example tool                                         | `cmd/wordcount/`                                                                           |
| Fly.io deployment (Dockerfile + fly.toml + startup script)      | `deploy/Dockerfile`, `deploy/fly.toml`, `deploy/start.sh`                                  |

### 🟡 Stubbed / Partial

| Feature                  | Notes                                                               |
| ------------------------ | ------------------------------------------------------------------- |
| Domain verification      | `verify` prints DNS TXT instructions but doesn't actually check DNS |
| Reputation recomputation | Static seed data — no cron/trigger to recompute from upvotes        |

### 🔜 Not Built Yet

| Feature                      | What's Missing                                                |
| ---------------------------- | ------------------------------------------------------------- |
| `cmd/toolshed/` CLI          | Cobra CLI for local use (`toolshed crawl`, `toolshed verify`) |
| `apps/mcp-server/`           | TypeScript MCP server wrapping SSH commands for discovery     |
| `docs/` directory            | Design docs, YAML spec reference                              |
| DoltHub publishing           | `dolt push` to DoltHub for public federation                  |
| Provider counter-attestation | Open question in doc, not implemented                         |

---

## Appendix A: Registry Schema (SQL)

```sql
-- Accounts (SSH key identity)
CREATE TABLE accounts (
    id VARCHAR(255) PRIMARY KEY,            -- key fingerprint: SHA256:nThbg6kXUpJW...
    domain VARCHAR(255),                    -- acme.com (verified via DNS TXT or .well-known)
    domain_verified BOOLEAN DEFAULT FALSE,
    display_name VARCHAR(255),
    is_provider BOOLEAN DEFAULT FALSE,
    key_type VARCHAR(32),                   -- ssh-ed25519, ssh-rsa, etc.
    public_key TEXT,                        -- full public key for verification
    first_seen DATETIME,
    last_seen DATETIME,
    created_at DATETIME,
    updated_at DATETIME
);

-- Tool definitions (immutable, content-addressed)
CREATE TABLE tool_definitions (
    content_hash VARCHAR(71) PRIMARY KEY,   -- sha256:... of (schema + invocation + capabilities + provider domain)
    provider_account VARCHAR(255) NOT NULL,
    provider_domain VARCHAR(255) NOT NULL,
    schema_json JSON NOT NULL,
    invocation_json JSON NOT NULL,
    capabilities_json JSON,
    created_at DATETIME,
    FOREIGN KEY (provider_account) REFERENCES accounts(id)
);

-- Tool listings (mutable, human-readable metadata)
CREATE TABLE tool_listings (
    id VARCHAR(255) PRIMARY KEY,            -- acme.com/fraud-detection
    definition_hash VARCHAR(71) NOT NULL,
    provider_account VARCHAR(255) NOT NULL,
    provider_domain VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    version_label VARCHAR(32),
    description TEXT,
    pricing_json JSON,
    payment_json JSON,
    source VARCHAR(32),                     -- 'crawl' (from .well-known)
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (definition_hash) REFERENCES tool_definitions(content_hash),
    FOREIGN KEY (provider_account) REFERENCES accounts(id)
);

-- Upvotes (proof-of-use quality signals — shared)
CREATE TABLE upvotes (
    id VARCHAR(255) PRIMARY KEY,
    tool_id VARCHAR(255) NOT NULL,
    key_fingerprint VARCHAR(255) NOT NULL,  -- SSH key that submitted the upvote
    invocation_id VARCHAR(255) NOT NULL,    -- linked invocation report
    invocation_hash VARCHAR(71) NOT NULL,   -- hash of the invocation record
    ledger_commit VARCHAR(255),             -- Dolt commit hash of the invocation
    quality_score INT,                      -- 1-5
    useful BOOLEAN,
    comment TEXT,
    created_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id),
    FOREIGN KEY (key_fingerprint) REFERENCES accounts(id)
);

-- Invocations / Reports (local to each node — never shared)
CREATE TABLE invocations (
    id VARCHAR(255) PRIMARY KEY,
    tool_id VARCHAR(255) NOT NULL,
    definition_hash VARCHAR(71) NOT NULL,
    key_fingerprint VARCHAR(255) NOT NULL,  -- SSH key that reported the call
    input_hash VARCHAR(71),
    output_hash VARCHAR(71),
    latency_ms INT,
    success BOOLEAN,
    created_at DATETIME
);

-- Reputation (computed, cached — materialized view)
CREATE TABLE reputation (
    tool_id VARCHAR(255) PRIMARY KEY,
    total_upvotes INT DEFAULT 0,
    verified_upvotes INT DEFAULT 0,
    avg_quality DECIMAL(3,2),
    unique_callers INT DEFAULT 0,
    total_reports INT DEFAULT 0,
    trend VARCHAR(16),
    computed_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id)
);
```

---

## Appendix B: YAML Spec

### Provider File (`/.well-known/toolshed.yaml`)

```yaml
# Required
version: "0.1"

# Provider identity (required)
provider:
  domain: acme.com # must match the domain hosting this file
  contact: tools@acme.com # optional, for humans

# Tools (one or more)
tools:
  - name: Fraud Detection # required, human-readable
    description: > # required
      Real-time transaction fraud scoring
      using machine learning models
    version: "3.1.0" # optional, cosmetic (like a git tag)
    capabilities: # required, used for search
      - fraud-detection
      - real-time
      - fintech
      - ml

    # How to call this tool (required)
    invoke:
      protocol: rest # rest | mcp | grpc | graphql
      endpoint: https://api.acme.com/fraud
      tool_name: fraud_detection # for protocols that multiplex (MCP, gRPC)

    # Input/output contract (required)
    schema:
      input:
        transaction_id: { type: string }
        amount: { type: number, min: 0 }
        merchant_category: { type: string }
      output:
        risk_score: { type: number, min: 0, max: 100 }
        flags: { type: array, items: { type: string } }

    # Pricing (optional, defaults to "free")
    pricing:
      model: free # free | per_call | subscription | contact

    # Payment methods (optional)
    payment:
      methods:
        - type: free
        - type: api_key
          signup_url: https://acme.com/developers
```

### Supported Field Types

| Type      | Properties   | Example                                    |
| --------- | ------------ | ------------------------------------------ |
| `string`  | —            | `{ type: string }`                         |
| `number`  | `min`, `max` | `{ type: number, min: 0, max: 100 }`       |
| `boolean` | —            | `{ type: boolean }`                        |
| `array`   | `items`      | `{ type: array, items: { type: string } }` |
| `object`  | —            | `{ type: object }`                         |

### Supported Pricing Models

| Model          | Meaning                                                |
| -------------- | ------------------------------------------------------ |
| `free`         | No cost. Call freely.                                  |
| `per_call`     | Charged per invocation. `price` and `currency` fields. |
| `subscription` | Monthly/annual plan. `signup_url` for details.         |
| `contact`      | Enterprise pricing. `contact_url` for humans.          |

### Supported Payment Methods

| Type             | Fields                   | Description                      |
| ---------------- | ------------------------ | -------------------------------- |
| `free`           | —                        | No payment needed                |
| `api_key`        | `signup_url`             | Get an API key from the provider |
| `stripe_connect` | `account_id`             | Pay via Stripe                   |
| `l402`           | `endpoint`, `price_sats` | Pay per call with Lightning      |
| `cashu`          | `mint`, `price_sats`     | Pay with ecash tokens            |

---

## Appendix C: Repo Structure

```
toolshed/
├── internal/
│   ├── core/             # ✅ Go: types, validation, content hashing, YAML parsing
│   ├── crawl/            # ✅ Go: .well-known/toolshed.yaml fetcher, domain verification
│   ├── dolt/             # ✅ Go: registry queries, ledger writes, schema migrations
│   └── ssh/              # ✅ Go: SSH command handlers, account management, bubbletea TUI
├── cmd/
│   ├── ssh/              # ✅ Go: SSH server entry point (wish)
│   ├── wordcount/        # ✅ Go: example tool provider (REST)
│   └── toolshed/         # 🔜 Go: CLI (cobra) — toolshed crawl, toolshed verify, etc.
├── apps/
│   └── mcp-server/       # 🔜 TypeScript: thin MCP server → calls SSH for discovery
├── deploy/
│   ├── Dockerfile        # ✅ Multi-stage: Go build → Dolt + SSH server runtime
│   ├── fly.toml          # ✅ Fly.io config: TCP service on 2222, persistent volume
│   └── start.sh          # ✅ Entrypoint: init Dolt DBs, start Dolt + SSH server
├── schema/
│   ├── registry/         # ✅ Dolt: definitions, listings, upvotes, accounts, reputation
│   └── ledger/           # ✅ Dolt: invocations (local, never shared)
├── scripts/              # ✅ Dolt bootstrap, config, seed data
├── docker-compose.yml    # ✅ Dolt dev environment
└── docs/                 # 🔜 Design docs, YAML spec reference
```
