-- ToolShed v2 Registry Seed Data
-- SSH key identity, no DID/Stripe, no SLA columns.
-- Word-count tool merged here (was previously 002_wordcount_seed.sql).

-- ============================================================
-- Accounts (SSH key identity)
-- ============================================================

INSERT INTO accounts (id, domain, domain_verified, display_name, is_provider, key_type, public_key, first_seen, last_seen, created_at, updated_at)
VALUES
    ('SHA256:test_acme_provider_key', 'acme.com', TRUE, 'Acme Corp', TRUE, 'ssh-ed25519', 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestAcmeProviderPublicKeyDataHere acme@acme.com', '2026-03-01 00:00:00', '2026-03-15 14:23:00', '2026-03-01 00:00:00', '2026-03-01 00:00:00'),
    ('SHA256:test_agent_key', 'agent-company-xyz.com', TRUE, 'Agent Company XYZ', FALSE, 'ssh-ed25519', 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestAgentPublicKeyDataHere agent@xyz.com', '2026-03-10 00:00:00', '2026-03-15 14:23:05', '2026-03-10 00:00:00', '2026-03-10 00:00:00'),
    ('SHA256:test_toolshed_dev_key', 'toolshed.dev', TRUE, 'ToolShed Examples', TRUE, 'ssh-ed25519', 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestToolShedDevPublicKeyDataHere dev@toolshed.dev', '2026-03-16 00:00:00', '2026-03-16 00:00:00', '2026-03-16 00:00:00', '2026-03-16 00:00:00');

-- ============================================================
-- Tool Definitions (immutable, content-addressed)
-- ============================================================

-- Fraud Detection (acme.com)
INSERT INTO tool_definitions (content_hash, provider_account, provider_domain, schema_json, invocation_json, capabilities_json, created_at)
VALUES (
    'sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2',
    'SHA256:test_acme_provider_key',
    'acme.com',
    '{"input":{"transaction_id":{"type":"string"},"amount":{"type":"number"},"merchant_category":{"type":"string"}},"output":{"risk_score":{"type":"number","min":0,"max":1},"flags":{"type":"array","items":{"type":"string"}}}}',
    '{"protocol":"rest","endpoint":"https://api.acme.com/fraud","tool_name":"fraud_check"}',
    '["fraud","ml","financial","real-time"]',
    '2026-03-01 00:00:00'
);

-- Word Count (toolshed.dev)
INSERT INTO tool_definitions (content_hash, provider_account, provider_domain, schema_json, invocation_json, capabilities_json, created_at)
VALUES (
    'sha256:d035f30e682cfefa3225540753f1c85f14d07bf2109bfde25a5e45d3b53a6928',
    'SHA256:test_toolshed_dev_key',
    'toolshed.dev',
    '{"input":{"text":{"type":"string"}},"output":{"words":{"type":"number"},"characters":{"type":"number"},"sentences":{"type":"number"},"paragraphs":{"type":"number"}}}',
    '{"protocol":"rest","endpoint":"http://localhost:9090","tool_name":"word_count"}',
    '["analysis","nlp","text","word-count"]',
    '2026-03-16 00:00:00'
);

-- ============================================================
-- Tool Listings (mutable metadata)
-- ============================================================

-- Fraud Detection listing
INSERT INTO tool_listings (id, definition_hash, provider_account, provider_domain, name, version_label, description, pricing_json, payment_json, source, created_at, updated_at)
VALUES (
    'acme.com/fraud-detection',
    'sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2',
    'SHA256:test_acme_provider_key',
    'acme.com',
    'Fraud Detection',
    '3.1.0',
    'Real-time transaction fraud scoring with ML',
    '{"model":"per_call","price":0.005,"currency":"usd"}',
    '{"methods":[{"type":"stripe_connect","account_id":"acct_acme_abc123"}]}',
    'push',
    '2026-03-01 00:00:00',
    '2026-03-01 00:00:00'
);

-- Word Count listing
INSERT INTO tool_listings (id, definition_hash, provider_account, provider_domain, name, version_label, description, pricing_json, payment_json, source, created_at, updated_at)
VALUES (
    'toolshed.dev/word-count',
    'sha256:d035f30e682cfefa3225540753f1c85f14d07bf2109bfde25a5e45d3b53a6928',
    'SHA256:test_toolshed_dev_key',
    'toolshed.dev',
    'Word Count',
    '1.0.0',
    'Counts words, characters, sentences, and paragraphs in text. Simple text analysis tool.',
    '{"model":"free","price":0,"currency":""}',
    '{"methods":[{"type":"free"}]}',
    'push',
    '2026-03-16 00:00:00',
    '2026-03-16 00:00:00'
);

-- ============================================================
-- Upvotes (proof-of-use quality signals)
-- ============================================================

-- Agent XYZ upvoted Fraud Detection after a real invocation
INSERT INTO upvotes (id, tool_id, key_fingerprint, invocation_id, invocation_hash, ledger_commit, quality_score, useful, comment, created_at)
VALUES (
    '5kqw3xmops7n2',
    'acme.com/fraud-detection',
    'SHA256:test_agent_key',
    'inv_fraud_001',
    'sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef',
    'dolt:76qerj11u38il8rb1ddjn3d6kivqamk2',
    5,
    TRUE,
    'Fast and accurate fraud scoring. Caught two suspicious transactions in our pipeline.',
    '2026-03-15 14:23:05'
);

-- Agent XYZ also tried Word Count
INSERT INTO upvotes (id, tool_id, key_fingerprint, invocation_id, invocation_hash, ledger_commit, quality_score, useful, comment, created_at)
VALUES (
    '8mrt5ynbqz2k7',
    'toolshed.dev/word-count',
    'SHA256:test_agent_key',
    'inv_wc_001',
    'sha256:cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe',
    'dolt:9xkwp42rv71hm3cq5aaef08snblj6yt4',
    4,
    TRUE,
    'Simple and reliable for text analysis tasks.',
    '2026-03-16 10:30:00'
);

-- ============================================================
-- Reputation (computed, cached — initial snapshots)
-- ============================================================

INSERT INTO reputation (tool_id, total_upvotes, verified_upvotes, avg_quality, unique_callers, total_reports, trend, computed_at)
VALUES
    ('acme.com/fraud-detection', 1, 1, 5.00, 1, 1, 'new', '2026-03-15 14:23:10'),
    ('toolshed.dev/word-count', 1, 1, 4.00, 1, 1, 'new', '2026-03-16 10:30:05');
