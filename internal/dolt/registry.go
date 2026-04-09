// Dolt registry queries and ledger writes for ToolShed v2.
package dolt

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/toolshed/toolshed/internal/core"
	"github.com/toolshed/toolshed/internal/embeddings"
)

// escapeLIKE escapes the SQL LIKE wildcard characters % and _ so that
// user-supplied search terms are matched literally. The escaped value
// uses backslash as the escape character, which is the default for MySQL
// (and therefore Dolt).
var likeSanitizer = strings.NewReplacer(
	`%`, `\%`,
	`_`, `\_`,
)

func escapeLIKE(s string) string {
	return likeSanitizer.Replace(s)
}

// ErrDuplicateUpvote is returned when a key tries to upvote the same
// invocation more than once (violates the UNIQUE constraint on
// upvotes(key_fingerprint, invocation_id)).
var ErrDuplicateUpvote = errors.New("dolt: duplicate upvote for this invocation")

// Registry provides access to the shared Dolt registry and the local ledger.
type Registry struct {
	registry *sql.DB
	ledger   *sql.DB
}

// NewRegistry opens connections to both databases and verifies connectivity.
func NewRegistry(registryDSN, ledgerDSN string) (*Registry, error) {
	registryDB, err := sql.Open("mysql", registryDSN)
	if err != nil {
		return nil, fmt.Errorf("dolt: open registry: %w", err)
	}
	if err := registryDB.Ping(); err != nil {
		registryDB.Close()
		return nil, fmt.Errorf("dolt: ping registry: %w", err)
	}

	ledgerDB, err := sql.Open("mysql", ledgerDSN)
	if err != nil {
		registryDB.Close()
		return nil, fmt.Errorf("dolt: open ledger: %w", err)
	}
	if err := ledgerDB.Ping(); err != nil {
		registryDB.Close()
		ledgerDB.Close()
		return nil, fmt.Errorf("dolt: ping ledger: %w", err)
	}

	return &Registry{
		registry: registryDB,
		ledger:   ledgerDB,
	}, nil
}

// Close closes both database connections.
func (r *Registry) Close() error {
	return errors.Join(r.registry.Close(), r.ledger.Close())
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

// GetOrCreateAccount upserts an account keyed by SSH key fingerprint.
// If the account already exists, last_seen and updated_at are bumped and the
// row is returned. Otherwise a new account is created with first_seen,
// last_seen, created_at, and updated_at all set to now.
func (r *Registry) GetOrCreateAccount(ctx context.Context, fingerprint, keyType, publicKey string) (*core.Account, error) {
	now := time.Now().UTC()

	const upsert = `
		INSERT INTO accounts
			(id, key_type, public_key, domain, domain_verified, display_name,
			 is_provider, first_seen, last_seen, created_at, updated_at)
		VALUES (?, ?, ?, '', FALSE, '', FALSE, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			last_seen  = VALUES(last_seen),
			updated_at = VALUES(updated_at)`

	_, err := r.registry.ExecContext(ctx, upsert,
		fingerprint, keyType, publicKey,
		now, now, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("dolt: upsert account %q: %w", fingerprint, err)
	}

	return r.GetAccount(ctx, fingerprint)
}

// GetAccount fetches an account by SSH key fingerprint.
// Returns (nil, nil) if not found.
func (r *Registry) GetAccount(ctx context.Context, fingerprint string) (*core.Account, error) {
	const query = `
		SELECT id, domain, domain_verified, display_name, is_provider,
		       key_type, public_key, first_seen, last_seen, created_at, updated_at
		FROM accounts
		WHERE id = ?`

	var (
		acct        core.Account
		domain      sql.NullString
		displayName sql.NullString
		keyType     sql.NullString
		publicKey   sql.NullString
	)

	err := r.registry.QueryRowContext(ctx, query, fingerprint).Scan(
		&acct.ID,
		&domain,
		&acct.DomainVerified,
		&displayName,
		&acct.IsProvider,
		&keyType,
		&publicKey,
		&acct.FirstSeen,
		&acct.LastSeen,
		&acct.CreatedAt,
		&acct.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dolt: get account %q: %w", fingerprint, err)
	}

	acct.Domain = domain.String
	acct.DisplayName = displayName.String
	acct.KeyType = keyType.String
	acct.PublicKey = publicKey.String

	return &acct, nil
}

// UpdateAccountDomain sets the domain for an account and marks it verified.
func (r *Registry) UpdateAccountDomain(ctx context.Context, fingerprint, domain string) error {
	const stmt = `
		UPDATE accounts
		SET domain = ?, domain_verified = TRUE, updated_at = ?
		WHERE id = ?`

	now := time.Now().UTC()
	res, err := r.registry.ExecContext(ctx, stmt, domain, now, fingerprint)
	if err != nil {
		return fmt.Errorf("dolt: update account domain %q: %w", fingerprint, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("dolt: update account domain rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("dolt: account %q not found", fingerprint)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tool Definitions (immutable, content-addressed)
// ---------------------------------------------------------------------------

// RegisterToolDefinition inserts an immutable tool definition. The insert is
// idempotent — if a row with the same content_hash already exists the
// statement is silently ignored (INSERT IGNORE).
func (r *Registry) RegisterToolDefinition(ctx context.Context, def core.ToolDefinition) error {
	schemaJSON, err := json.Marshal(def.Schema)
	if err != nil {
		return fmt.Errorf("dolt: marshal schema: %w", err)
	}
	invocationJSON, err := json.Marshal(def.Invocation)
	if err != nil {
		return fmt.Errorf("dolt: marshal invocation: %w", err)
	}
	capabilitiesJSON, err := json.Marshal(def.Capabilities)
	if err != nil {
		return fmt.Errorf("dolt: marshal capabilities: %w", err)
	}

	const stmt = `
		INSERT IGNORE INTO tool_definitions
			(content_hash, provider_account, provider_domain,
			 schema_json, invocation_json, capabilities_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err = r.registry.ExecContext(ctx, stmt,
		def.ContentHash,
		def.Provider.Contact, // provider_account (fingerprint passed via Contact)
		def.Provider.Domain,
		schemaJSON,
		invocationJSON,
		capabilitiesJSON,
		def.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("dolt: register tool definition %q: %w", def.ContentHash, err)
	}
	return nil
}

// GetToolDefinition fetches an immutable tool definition by content hash.
// Returns (nil, nil) if not found.
func (r *Registry) GetToolDefinition(ctx context.Context, contentHash string) (*core.ToolDefinition, error) {
	const query = `
		SELECT content_hash, provider_account, provider_domain,
		       schema_json, invocation_json, capabilities_json, created_at
		FROM tool_definitions
		WHERE content_hash = ?`

	var (
		def             core.ToolDefinition
		providerAccount sql.NullString
		schemaRaw       []byte
		invocationRaw   []byte
		capabilitiesRaw []byte
	)

	err := r.registry.QueryRowContext(ctx, query, contentHash).Scan(
		&def.ContentHash,
		&providerAccount,
		&def.Provider.Domain,
		&schemaRaw,
		&invocationRaw,
		&capabilitiesRaw,
		&def.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dolt: get tool definition %q: %w", contentHash, err)
	}

	def.Provider.Contact = providerAccount.String

	if err := json.Unmarshal(schemaRaw, &def.Schema); err != nil {
		return nil, fmt.Errorf("dolt: unmarshal schema for %q: %w", contentHash, err)
	}
	if err := json.Unmarshal(invocationRaw, &def.Invocation); err != nil {
		return nil, fmt.Errorf("dolt: unmarshal invocation for %q: %w", contentHash, err)
	}
	if len(capabilitiesRaw) > 0 {
		if err := json.Unmarshal(capabilitiesRaw, &def.Capabilities); err != nil {
			return nil, fmt.Errorf("dolt: unmarshal capabilities for %q: %w", contentHash, err)
		}
	}

	return &def, nil
}

// ---------------------------------------------------------------------------
// Tool Listings (mutable metadata)
// ---------------------------------------------------------------------------

// RegisterToolListing upserts a tool listing. If a listing with the same ID
// already exists it is updated; otherwise a new row is inserted.
func (r *Registry) RegisterToolListing(ctx context.Context, listing core.ToolListing) error {
	pricingJSON, err := json.Marshal(listing.Pricing)
	if err != nil {
		return fmt.Errorf("dolt: marshal pricing: %w", err)
	}
	paymentJSON, err := json.Marshal(listing.Payment)
	if err != nil {
		return fmt.Errorf("dolt: marshal payment: %w", err)
	}

	const stmt = `
		INSERT INTO tool_listings
			(id, definition_hash, provider_account, provider_domain,
			 name, version_label, description, pricing_json, payment_json,
			 source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			definition_hash  = VALUES(definition_hash),
			provider_account = VALUES(provider_account),
			provider_domain  = VALUES(provider_domain),
			name             = VALUES(name),
			version_label    = VALUES(version_label),
			description      = VALUES(description),
			pricing_json     = VALUES(pricing_json),
			payment_json     = VALUES(payment_json),
			source           = VALUES(source),
			updated_at       = VALUES(updated_at)`

	now := time.Now().UTC()
	createdAt := listing.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := listing.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}

	_, err = r.registry.ExecContext(ctx, stmt,
		listing.ID,
		listing.DefinitionHash,
		listing.ProviderAccount,
		listing.ProviderDomain,
		listing.Name,
		listing.VersionLabel,
		listing.Description,
		pricingJSON,
		paymentJSON,
		listing.Source,
		createdAt,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("dolt: register tool listing %q: %w", listing.ID, err)
	}
	return nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows, allowing a single
// scan helper to be shared across QueryRow and Query result sets.
type scanner interface {
	Scan(dest ...any) error
}

func scanToolListing(s scanner) (core.ToolListing, error) {
	var (
		listing                                       core.ToolListing
		providerAccount, version, description, source sql.NullString
		pricingRaw, paymentRaw                        []byte
	)
	if err := s.Scan(
		&listing.ID, &listing.DefinitionHash, &providerAccount,
		&listing.ProviderDomain, &listing.Name, &version,
		&description, &pricingRaw, &paymentRaw, &source,
		&listing.CreatedAt, &listing.UpdatedAt,
	); err != nil {
		return core.ToolListing{}, err
	}
	listing.ProviderAccount = providerAccount.String
	listing.VersionLabel = version.String
	listing.Description = description.String
	listing.Source = source.String
	if err := json.Unmarshal(pricingRaw, &listing.Pricing); err != nil {
		return core.ToolListing{}, fmt.Errorf("unmarshal pricing: %w", err)
	}
	if err := json.Unmarshal(paymentRaw, &listing.Payment); err != nil {
		return core.ToolListing{}, fmt.Errorf("unmarshal payment: %w", err)
	}
	return listing, nil
}

// GetToolListing fetches a single tool listing by ID.
// Returns (nil, nil) if not found.
func (r *Registry) GetToolListing(ctx context.Context, toolID string) (*core.ToolListing, error) {
	const query = `
		SELECT id, definition_hash, provider_account, provider_domain,
		       name, version_label, description, pricing_json, payment_json,
		       source, created_at, updated_at
		FROM tool_listings
		WHERE id = ?`

	listing, err := scanToolListing(r.registry.QueryRowContext(ctx, query, toolID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dolt: get tool listing %q: %w", toolID, err)
	}

	return &listing, nil
}

// SearchTools searches tool listings by name, description, or capabilities.
// The query string is matched with SQL LIKE against name, description, and
// the capabilities JSON in the joined tool_definitions table.
func (r *Registry) SearchTools(ctx context.Context, query string) ([]core.ToolListing, error) {
	const stmt = `
		SELECT DISTINCT tl.id, tl.definition_hash, tl.provider_account,
		       tl.provider_domain, tl.name, tl.version_label, tl.description,
		       tl.pricing_json, tl.payment_json, tl.source,
		       tl.created_at, tl.updated_at
		FROM tool_listings tl
		JOIN tool_definitions td ON tl.definition_hash = td.content_hash
		WHERE tl.name LIKE ?
		   OR tl.description LIKE ?
		   OR td.capabilities_json LIKE ?
		ORDER BY tl.name`

	pattern := "%" + escapeLIKE(query) + "%"

	rows, err := r.registry.QueryContext(ctx, stmt, pattern, pattern, pattern)
	if err != nil {
		return nil, fmt.Errorf("dolt: search tools %q: %w", query, err)
	}
	defer rows.Close()

	var results []core.ToolListing
	for rows.Next() {
		listing, err := scanToolListing(rows)
		if err != nil {
			return nil, fmt.Errorf("dolt: scan search result: %w", err)
		}
		results = append(results, listing)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dolt: search tools iteration: %w", err)
	}

	return results, nil
}

// ListToolsByProvider returns all tool listings for a given provider domain.
func (r *Registry) ListToolsByProvider(ctx context.Context, providerDomain string) ([]core.ToolListing, error) {
	const stmt = `
		SELECT id, definition_hash, provider_account, provider_domain,
		       name, version_label, description, pricing_json, payment_json,
		       source, created_at, updated_at
		FROM tool_listings
		WHERE provider_domain = ?
		ORDER BY name`

	rows, err := r.registry.QueryContext(ctx, stmt, providerDomain)
	if err != nil {
		return nil, fmt.Errorf("dolt: list tools by provider %q: %w", providerDomain, err)
	}
	defer rows.Close()

	var results []core.ToolListing
	for rows.Next() {
		listing, err := scanToolListing(rows)
		if err != nil {
			return nil, fmt.Errorf("dolt: scan provider listing: %w", err)
		}
		results = append(results, listing)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dolt: list tools by provider iteration: %w", err)
	}

	return results, nil
}

// ---------------------------------------------------------------------------
// Reputation
// ---------------------------------------------------------------------------

// GetReputation fetches the computed reputation snapshot for a tool.
// Returns (nil, nil) if not found.
func (r *Registry) GetReputation(ctx context.Context, toolID string) (*core.Reputation, error) {
	const query = `
		SELECT tool_id, total_upvotes, verified_upvotes, avg_quality,
		       unique_callers, total_reports, trend, computed_at
		FROM reputation
		WHERE tool_id = ?`

	var (
		rep   core.Reputation
		trend sql.NullString
	)

	err := r.registry.QueryRowContext(ctx, query, toolID).Scan(
		&rep.ToolID,
		&rep.TotalUpvotes,
		&rep.VerifiedUpvotes,
		&rep.AvgQuality,
		&rep.UniqueCallers,
		&rep.TotalReports,
		&trend,
		&rep.ComputedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dolt: get reputation %q: %w", toolID, err)
	}

	rep.Trend = trend.String

	return &rep, nil
}

// ---------------------------------------------------------------------------
// Dolt commit helper
// ---------------------------------------------------------------------------

// doltCommit stages the given table and creates a Dolt commit on the specified
// database connection, returning the commit hash. This makes the write visible
// in the Dolt commit log and captures the hash for audit / tamper evidence.
func (r *Registry) doltCommit(ctx context.Context, db *sql.DB, table, message string) (string, error) {
	// Pin both DOLT_ADD and DOLT_COMMIT to the same connection.
	// Dolt's staging area is per-session, so using the connection pool
	// directly could dispatch the two calls to different connections,
	// resulting in empty or incorrect commits.
	conn, err := db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("dolt: acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "CALL DOLT_ADD(?)", table); err != nil {
		return "", fmt.Errorf("dolt: add %q: %w", table, err)
	}
	var hash string
	err = conn.QueryRowContext(ctx, "CALL DOLT_COMMIT('-m', ?)", message).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("dolt: commit %q: %w", message, err)
	}
	return hash, nil
}

// ---------------------------------------------------------------------------
// Invocations (local ledger)
// ---------------------------------------------------------------------------

// WriteInvocation inserts an invocation record into the local ledger.
func (r *Registry) WriteInvocation(ctx context.Context, inv core.InvocationRecord) (string, error) {
	const stmt = `
		INSERT INTO invocations
			(id, tool_id, definition_hash, key_fingerprint,
			 input_hash, output_hash, latency_ms, success, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.ledger.ExecContext(ctx, stmt,
		inv.ID,
		inv.ToolID,
		inv.DefinitionHash,
		inv.KeyFingerprint,
		inv.InputHash,
		inv.OutputHash,
		inv.LatencyMs,
		inv.Success,
		inv.CreatedAt,
	)
	if err != nil {
		return "", fmt.Errorf("dolt: write invocation %q: %w", inv.ID, err)
	}

	commitMsg := fmt.Sprintf("invocation: %s for %s by %s", inv.ID, inv.ToolID, inv.KeyFingerprint)
	hash, err := r.doltCommit(ctx, r.ledger, "invocations", commitMsg)
	if err != nil {
		return "", fmt.Errorf("dolt: commit invocation %q: %w", inv.ID, err)
	}
	return hash, nil
}

// GetInvocationByKeyAndTool looks up an invocation record for a given SSH
// key fingerprint and tool ID. This is used during upvote validation to
// confirm that the caller actually invoked the tool before rating it.
// Returns (nil, nil) if no matching record exists.
func scanInvocationRecord(s scanner) (core.InvocationRecord, error) {
	var (
		inv        core.InvocationRecord
		inputHash  sql.NullString
		outputHash sql.NullString
	)
	if err := s.Scan(
		&inv.ID, &inv.ToolID, &inv.DefinitionHash, &inv.KeyFingerprint,
		&inputHash, &outputHash, &inv.LatencyMs, &inv.Success, &inv.CreatedAt,
	); err != nil {
		return core.InvocationRecord{}, err
	}
	inv.InputHash = inputHash.String
	inv.OutputHash = outputHash.String
	return inv, nil
}

func (r *Registry) GetInvocationByKeyAndTool(ctx context.Context, keyFingerprint, toolID string) (*core.InvocationRecord, error) {
	const query = `
		SELECT id, tool_id, definition_hash, key_fingerprint,
		       input_hash, output_hash, latency_ms, success, created_at
		FROM invocations
		WHERE key_fingerprint = ? AND tool_id = ?
		ORDER BY created_at DESC
		LIMIT 1`

	inv, err := scanInvocationRecord(r.ledger.QueryRowContext(ctx, query, keyFingerprint, toolID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dolt: get invocation by key %q tool %q: %w", keyFingerprint, toolID, err)
	}

	return &inv, nil
}

// GetInvocationByID looks up an invocation record by its ID and verifies
// that it belongs to the given SSH key fingerprint. Returns (nil, nil) if
// not found or if the fingerprint doesn't match.
func (r *Registry) GetInvocationByID(ctx context.Context, invocationID, keyFingerprint string) (*core.InvocationRecord, error) {
	const query = `
		SELECT id, tool_id, definition_hash, key_fingerprint,
		       input_hash, output_hash, latency_ms, success, created_at
		FROM invocations
		WHERE id = ? AND key_fingerprint = ?`

	inv, err := scanInvocationRecord(r.ledger.QueryRowContext(ctx, query, invocationID, keyFingerprint))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dolt: get invocation %q for key %q: %w", invocationID, keyFingerprint, err)
	}

	return &inv, nil
}

// GetInvocationLedgerCommit returns the Dolt commit hash from the ledger that
// recorded a specific invocation. It queries dolt_log for the commit whose
// message contains the invocation ID. Returns ("", nil) if not found.
func (r *Registry) GetInvocationLedgerCommit(ctx context.Context, invocationID string) (string, error) {
	const query = `
		SELECT commit_hash
		FROM dolt_log
		WHERE message LIKE ?
		ORDER BY date DESC
		LIMIT 1`
	pattern := fmt.Sprintf("%%invocation: %s%%", invocationID)

	var hash string
	err := r.ledger.QueryRowContext(ctx, query, pattern).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("dolt: get ledger commit for invocation %q: %w", invocationID, err)
	}
	return hash, nil
}

// AuditEntry represents a single Dolt commit from the registry audit log.
type AuditEntry struct {
	CommitHash string    `json:"commit_hash" yaml:"commit_hash"`
	Message    string    `json:"message" yaml:"message"`
	Committer  string    `json:"committer" yaml:"committer"`
	Date       time.Time `json:"date" yaml:"date"`
}

// GetAuditLog returns Dolt commits from the registry whose messages mention
// the given tool ID. This provides a verifiable history of all upvotes and
// reputation changes for a tool — anyone can verify it by cloning the registry.
func (r *Registry) GetAuditLog(ctx context.Context, toolID string, limit int) ([]AuditEntry, error) {
	const query = `
		SELECT commit_hash, committer, date, message
		FROM dolt_log
		WHERE message LIKE ?
		ORDER BY date DESC
		LIMIT ?`
	pattern := fmt.Sprintf("%%%s%%", toolID)

	rows, err := r.registry.QueryContext(ctx, query, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("dolt: get audit log for %q: %w", toolID, err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.CommitHash, &e.Committer, &e.Date, &e.Message); err != nil {
			return nil, fmt.Errorf("dolt: scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dolt: audit log rows: %w", err)
	}

	return entries, nil
}

// ---------------------------------------------------------------------------
// Upvotes
// ---------------------------------------------------------------------------

// WriteUpvote inserts an upvote into the shared registry. The upvote is a
// flat record linked to an invocation report via invocation_id and
// invocation_hash, and tied to an SSH key identity via key_fingerprint.
func (r *Registry) WriteUpvote(ctx context.Context, upvote core.Upvote) error {
	const stmt = `
		INSERT INTO upvotes
			(id, tool_id, key_fingerprint, invocation_id, invocation_hash,
			 ledger_commit, quality_score, useful, comment, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.registry.ExecContext(ctx, stmt,
		upvote.ID,
		upvote.ToolID,
		upvote.KeyFingerprint,
		upvote.InvocationID,
		upvote.InvocationHash,
		upvote.LedgerCommit,
		upvote.QualityScore,
		upvote.Useful,
		upvote.Comment,
		upvote.CreatedAt,
	)
	if err != nil {
		// MySQL error 1062 = duplicate entry. The UNIQUE index on
		// (key_fingerprint, invocation_id) fires when the same key
		// tries to upvote the same invocation twice.
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ErrDuplicateUpvote
		}
		return fmt.Errorf("dolt: write upvote %q: %w", upvote.ID, err)
	}

	commitMsg := fmt.Sprintf("upvote: %s rated %s %d/5", upvote.KeyFingerprint, upvote.ToolID, upvote.QualityScore)
	if _, err := r.doltCommit(ctx, r.registry, "upvotes", commitMsg); err != nil {
		return fmt.Errorf("dolt: commit upvote %q: %w", upvote.ID, err)
	}
	return nil
}

// CountUpvotesByKeyAndTool returns how many upvotes a specific key has cast
// for a given tool. This is used for budget checks (rate-limiting upvotes
// per key per tool).
func (r *Registry) CountUpvotesByKeyAndTool(ctx context.Context, keyFingerprint, toolID string) (int, error) {
	const q = `SELECT COUNT(*) FROM upvotes WHERE key_fingerprint = ? AND tool_id = ?`
	var count int
	if err := r.registry.QueryRowContext(ctx, q, keyFingerprint, toolID).Scan(&count); err != nil {
		return 0, fmt.Errorf("dolt: count upvotes for key %q tool %q: %w", keyFingerprint, toolID, err)
	}
	return count, nil
}

// RecomputeReputation recalculates the reputation row for a given tool by
// aggregating from the upvotes table, then upserts the result and creates
// an explicit Dolt commit.
func (r *Registry) RecomputeReputation(ctx context.Context, toolID string) error {
	const aggQuery = `
		SELECT
			COUNT(*) as total_upvotes,
			COUNT(CASE WHEN invocation_id IS NOT NULL THEN 1 END) as verified_upvotes,
			AVG(quality_score) as avg_quality,
			COUNT(DISTINCT key_fingerprint) as unique_callers
		FROM upvotes
		WHERE tool_id = ?`

	var (
		totalUpvotes    int
		verifiedUpvotes int
		avgQuality      float64
		uniqueCallers   int
	)

	err := r.registry.QueryRowContext(ctx, aggQuery, toolID).Scan(
		&totalUpvotes,
		&verifiedUpvotes,
		&avgQuality,
		&uniqueCallers,
	)
	if err != nil {
		return fmt.Errorf("dolt: aggregate reputation for %q: %w", toolID, err)
	}

	// Count total invocation reports from local ledger for this tool.
	var totalReports int
	err = r.ledger.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM invocations WHERE tool_id = ?", toolID,
	).Scan(&totalReports)
	if err != nil {
		return fmt.Errorf("dolt: count reports for %q: %w", toolID, err)
	}

	// Determine trend based on recent upvotes vs overall.
	trend := "new"
	if totalUpvotes > 0 {
		trend = "stable"
	}

	now := time.Now().UTC()

	const upsertStmt = `
		INSERT INTO reputation
			(tool_id, total_upvotes, verified_upvotes, avg_quality,
			 unique_callers, total_reports, trend, computed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			total_upvotes = VALUES(total_upvotes),
			verified_upvotes = VALUES(verified_upvotes),
			avg_quality = VALUES(avg_quality),
			unique_callers = VALUES(unique_callers),
			total_reports = VALUES(total_reports),
			trend = VALUES(trend),
			computed_at = VALUES(computed_at)`

	_, err = r.registry.ExecContext(ctx, upsertStmt,
		toolID, totalUpvotes, verifiedUpvotes, avgQuality,
		uniqueCallers, totalReports, trend, now,
	)
	if err != nil {
		return fmt.Errorf("dolt: upsert reputation for %q: %w", toolID, err)
	}

	commitMsg := fmt.Sprintf("reputation: recomputed %s (avg %.1f, %d verified)", toolID, avgQuality, verifiedUpvotes)
	if _, err := r.doltCommit(ctx, r.registry, "reputation", commitMsg); err != nil {
		return fmt.Errorf("dolt: commit reputation for %q: %w", toolID, err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Embeddings (semantic search support)
// ---------------------------------------------------------------------------

// StoreEmbedding stores or updates a tool's embedding vector in the registry.
// The embedding is binary-encoded (little-endian float32) for compact storage.
func (r *Registry) StoreEmbedding(ctx context.Context, te embeddings.ToolEmbedding) error {
	const stmt = `
		INSERT INTO tool_embeddings
			(tool_id, embedding, model, dimensions, text_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			embedding = VALUES(embedding),
			model = VALUES(model),
			dimensions = VALUES(dimensions),
			text_hash = VALUES(text_hash),
			updated_at = VALUES(updated_at)`

	encoded := embeddings.EncodeEmbedding(te.Embedding)
	now := time.Now()

	_, err := r.registry.ExecContext(ctx, stmt,
		te.ToolID,
		encoded,
		te.Model,
		te.Dimensions,
		te.TextHash,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("dolt: store embedding for %q: %w", te.ToolID, err)
	}
	return nil
}

// GetAllEmbeddings loads all tool embeddings from the registry. This is used
// for in-memory cosine similarity search. For registries with thousands of
// tools, this should be cached and refreshed periodically.
func (r *Registry) GetAllEmbeddings(ctx context.Context) ([]embeddings.ToolEmbedding, error) {
	const query = `
		SELECT tool_id, embedding, model, dimensions, text_hash
		FROM tool_embeddings`

	rows, err := r.registry.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("dolt: get all embeddings: %w", err)
	}
	defer rows.Close()

	var results []embeddings.ToolEmbedding
	for rows.Next() {
		var (
			te       embeddings.ToolEmbedding
			encoded  []byte
			textHash sql.NullString
		)

		if err := rows.Scan(&te.ToolID, &encoded, &te.Model, &te.Dimensions, &textHash); err != nil {
			return nil, fmt.Errorf("dolt: scan embedding: %w", err)
		}

		te.Embedding = embeddings.DecodeEmbedding(encoded)
		te.TextHash = textHash.String
		results = append(results, te)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dolt: get all embeddings iteration: %w", err)
	}

	return results, nil
}

// GetToolListingsByIDs fetches multiple tool listings by their IDs. This is
// used to hydrate semantic search results (which only have tool IDs + scores).
//
// NOTE: The returned slice is NOT ordered to match the input `ids` slice.
// Callers that care about ordering (e.g. preserving similarity ranking)
// must re-sort the results themselves.
func (r *Registry) GetToolListingsByIDs(ctx context.Context, ids []string) ([]core.ToolListing, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build a query with placeholders for each ID.
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	query := `
		SELECT id, definition_hash, provider_account, provider_domain,
		       name, version_label, description, pricing_json, payment_json,
		       source, created_at, updated_at
		FROM tool_listings
		WHERE id IN (` + placeholders + `)`

	rows, err := r.registry.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("dolt: get listings by ids: %w", err)
	}
	defer rows.Close()

	var results []core.ToolListing
	for rows.Next() {
		listing, err := scanToolListing(rows)
		if err != nil {
			return nil, fmt.Errorf("dolt: scan listing by id: %w", err)
		}
		results = append(results, listing)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dolt: get listings by ids iteration: %w", err)
	}

	return results, nil
}

// ToolWithDefinition pairs a listing with its definition — used by the
// embedding backfill to build the text that gets embedded.
type ToolWithDefinition struct {
	Listing    core.ToolListing
	Definition core.ToolDefinition
}

// GetToolsMissingEmbeddings returns all tool listings (joined with their
// definitions) that do not yet have an entry in tool_embeddings. This is
// used on startup to backfill embeddings for seeded or previously-registered
// tools.
func (r *Registry) GetToolsMissingEmbeddings(ctx context.Context) ([]ToolWithDefinition, error) {
	const query = `
		SELECT tl.id, tl.definition_hash, tl.provider_account, tl.provider_domain,
		       tl.name, tl.version_label, tl.description, tl.pricing_json, tl.payment_json,
		       tl.source, tl.created_at, tl.updated_at,
		       td.schema_json, td.invocation_json, td.capabilities_json, td.created_at
		FROM tool_listings tl
		JOIN tool_definitions td ON tl.definition_hash = td.content_hash
		LEFT JOIN tool_embeddings te ON tl.id = te.tool_id
		WHERE te.tool_id IS NULL`

	rows, err := r.registry.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("dolt: get tools missing embeddings: %w", err)
	}
	defer rows.Close()

	var results []ToolWithDefinition
	for rows.Next() {
		var (
			tw              ToolWithDefinition
			providerAccount sql.NullString
			version         sql.NullString
			description     sql.NullString
			source          sql.NullString
			pricingRaw      []byte
			paymentRaw      []byte
			schemaRaw       []byte
			invocationRaw   []byte
			capabilitiesRaw []byte
			defCreatedAt    time.Time
		)

		if err := rows.Scan(
			&tw.Listing.ID,
			&tw.Listing.DefinitionHash,
			&providerAccount,
			&tw.Listing.ProviderDomain,
			&tw.Listing.Name,
			&version,
			&description,
			&pricingRaw,
			&paymentRaw,
			&source,
			&tw.Listing.CreatedAt,
			&tw.Listing.UpdatedAt,
			&schemaRaw,
			&invocationRaw,
			&capabilitiesRaw,
			&defCreatedAt,
		); err != nil {
			return nil, fmt.Errorf("dolt: scan tool missing embedding: %w", err)
		}

		tw.Listing.ProviderAccount = providerAccount.String
		tw.Listing.VersionLabel = version.String
		tw.Listing.Description = description.String
		tw.Listing.Source = source.String

		if err := json.Unmarshal(pricingRaw, &tw.Listing.Pricing); err != nil {
			return nil, fmt.Errorf("dolt: unmarshal pricing: %w", err)
		}
		if err := json.Unmarshal(paymentRaw, &tw.Listing.Payment); err != nil {
			return nil, fmt.Errorf("dolt: unmarshal payment: %w", err)
		}

		tw.Definition.ContentHash = tw.Listing.DefinitionHash
		tw.Definition.Provider = core.Provider{Domain: tw.Listing.ProviderDomain}
		tw.Definition.CreatedAt = defCreatedAt

		if err := json.Unmarshal(schemaRaw, &tw.Definition.Schema); err != nil {
			return nil, fmt.Errorf("dolt: unmarshal schema: %w", err)
		}
		if err := json.Unmarshal(invocationRaw, &tw.Definition.Invocation); err != nil {
			return nil, fmt.Errorf("dolt: unmarshal invocation: %w", err)
		}
		if capabilitiesRaw != nil {
			if err := json.Unmarshal(capabilitiesRaw, &tw.Definition.Capabilities); err != nil {
				return nil, fmt.Errorf("dolt: unmarshal capabilities: %w", err)
			}
		}

		results = append(results, tw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dolt: get tools missing embeddings iteration: %w", err)
	}

	return results, nil
}
