-- ToolShed Registry: Embeddings Support
-- Stores vector embeddings for semantic search over tool listings.
-- Embeddings are generated via OpenAI (text-embedding-3-small) or compatible APIs.
-- Cosine similarity is computed in the application layer (Go), not in SQL.

CREATE TABLE tool_embeddings (
    tool_id VARCHAR(255) PRIMARY KEY,
    embedding BLOB NOT NULL,                -- float32 vector, little-endian binary (4 bytes × dimensions)
    model VARCHAR(128) NOT NULL,            -- e.g. "text-embedding-3-small"
    dimensions INT NOT NULL,                -- e.g. 1536
    text_hash VARCHAR(71),                  -- sha256 of the embedded text (for staleness detection)
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (tool_id) REFERENCES tool_listings(id)
);
