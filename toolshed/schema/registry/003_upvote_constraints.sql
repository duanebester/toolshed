-- 003_upvote_constraints.sql
--
-- Adds indexes to the upvotes table for correctness and performance:
--
-- 1. A UNIQUE index on (key_fingerprint, invocation_id) to prevent the same
--    key from upvoting the same invocation more than once.
--
-- 2. A covering index on (key_fingerprint, tool_id) to support efficient
--    budget checks — i.e. counting how many upvotes a given key has cast
--    for a particular tool.

CREATE UNIQUE INDEX idx_upvotes_key_invocation
    ON upvotes (key_fingerprint, invocation_id);

CREATE INDEX idx_upvotes_key_tool
    ON upvotes (key_fingerprint, tool_id);
