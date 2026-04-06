-- ToolShed Registry Schema v2
-- Shared database: cloned and replicated across nodes via Dolt.
-- Identity model: SSH public key fingerprints (no Stripe, no DID).
-- ToolShed is "DNS, not a CDN" — it never proxies tool calls.

-- ============================================================
-- Accounts (SSH key identity)
-- ============================================================

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

CREATE INDEX idx_accounts_domain ON accounts(domain);
CREATE INDEX idx_accounts_domain_verified ON accounts(domain, domain_verified);
CREATE INDEX idx_accounts_is_provider ON accounts(is_provider);

-- ============================================================
-- Tool definitions (immutable, content-addressed)
-- ============================================================
-- The content_hash is sha256 of (schema + invocation + capabilities + provider domain).
-- Once written, never updated, never deleted.

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

CREATE INDEX idx_tool_definitions_provider ON tool_definitions(provider_account);
CREATE INDEX idx_tool_definitions_domain ON tool_definitions(provider_domain);

-- ============================================================
-- Tool listings (mutable, human-readable metadata)
-- ============================================================
-- Points to an immutable definition via definition_hash.
-- Name, pricing, description can be updated freely by the provider.

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
    source VARCHAR(32),                     -- 'push' (registered via SSH/API) or 'crawl' (from .well-known)
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (definition_hash) REFERENCES tool_definitions(content_hash),
    FOREIGN KEY (provider_account) REFERENCES accounts(id)
);

CREATE INDEX idx_tool_listings_definition ON tool_listings(definition_hash);
CREATE INDEX idx_tool_listings_provider ON tool_listings(provider_account);
CREATE INDEX idx_tool_listings_domain ON tool_listings(provider_domain);
CREATE INDEX idx_tool_listings_name ON tool_listings(name);
CREATE INDEX idx_tool_listings_source ON tool_listings(source);

-- ============================================================
-- Upvotes (proof-of-use quality signals — shared)
-- ============================================================
-- Each upvote links to a specific invocation in the ledger.
-- Identity is SSH key fingerprint. Sybil-resistant by design.

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

CREATE INDEX idx_upvotes_tool ON upvotes(tool_id);
CREATE INDEX idx_upvotes_key_fingerprint ON upvotes(key_fingerprint);
CREATE INDEX idx_upvotes_invocation ON upvotes(invocation_id);

-- ============================================================
-- Reputation (computed, cached — materialized view)
-- ============================================================
-- Periodically recomputed. Never written directly by users.

CREATE TABLE reputation (
    tool_id VARCHAR(255) PRIMARY KEY,
    total_upvotes INT DEFAULT 0,
    verified_upvotes INT DEFAULT 0,
    avg_quality DECIMAL(3,2),
    unique_callers INT DEFAULT 0,
    total_reports INT DEFAULT 0,
    trend VARCHAR(16),                      -- "rising", "stable", "declining", "new"
    computed_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id)
);
