-- ToolShed Ledger Schema v2
-- Local to each node — never shared or cloned.
-- Tracks invocations: who called what, when, and did it work.
-- Identity is SSH key fingerprints (no Stripe/DID).

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

CREATE INDEX idx_invocations_tool ON invocations(tool_id);
CREATE INDEX idx_invocations_definition ON invocations(definition_hash);
CREATE INDEX idx_invocations_key ON invocations(key_fingerprint);
CREATE INDEX idx_invocations_created ON invocations(created_at);
