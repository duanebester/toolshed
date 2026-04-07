// Command handlers for the ToolShed SSH server.
//
// Each command is invoked via SSH in non-interactive mode:
//
//	ssh toolshed.sh search "fraud detection"
//	ssh toolshed.sh info acme.com/fraud-detection
//	ssh toolshed.sh report --tool acme.com/fraud-detection --latency 120 --success
//	ssh toolshed.sh upvote acme.com/fraud-detection --quality 5 --useful --comment "great"
//	ssh toolshed.sh verify acme.com
//
// All successful output is compact JSON written to stdout. Errors go to stderr.
package ssh

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/google/uuid"

	"github.com/toolshed/toolshed/internal/core"
	"github.com/toolshed/toolshed/internal/crawl"
	"github.com/toolshed/toolshed/internal/dolt"
	"github.com/toolshed/toolshed/internal/embeddings"
)

// maxUpvotesPerTool is the maximum number of upvotes a single key can cast
// for a given tool. Each upvote must reference a distinct invocation.
const maxUpvotesPerTool = 5

// CommandDispatcher routes SSH commands to their handlers.
type CommandDispatcher struct {
	registry    *dolt.Registry
	embedder    embeddings.Embedder // nil = semantic search disabled
	fingerprint string              // SSH key fingerprint of the connected user
	cmdLimiter  *CommandRateLimiter // per-fingerprint command rate limiting (shared across sessions)
}

// Dispatch parses the command name from the first argument and delegates
// to the appropriate handler. Unknown commands produce an error on stderr.
func (d *CommandDispatcher) Dispatch(sess ssh.Session, cmd []string) {
	if len(cmd) == 0 {
		fmt.Fprintf(sess.Stderr(), "error: no command provided\n")
		return
	}

	name := cmd[0]
	args := cmd[1:]

	switch name {
	case "search":
		d.handleSearch(sess, args)
	case "info":
		d.handleInfo(sess, args)
	case "report":
		d.handleReport(sess, args)
	case "upvote":
		d.handleUpvote(sess, args)
	case "verify":
		d.handleVerify(sess, args)
	case "crawl":
		d.handleCrawl(sess, args)
	case "audit":
		d.handleAudit(sess, args)
	case "reputation":
		d.handleReputation(sess, args)
	case "help":
		d.handleHelp(sess, args)
	default:
		fmt.Fprintf(sess.Stderr(), "error: unknown command %q\n", name)
		fmt.Fprintf(sess.Stderr(), "available commands: search, info, report, upvote, verify, crawl, audit, reputation, help\n")
	}
}

type helpResponse struct {
	Version     string        `json:"version" yaml:"version"`
	Description string        `json:"description" yaml:"description"`
	Commands    []helpCommand `json:"commands" yaml:"commands"`
	Interactive string        `json:"interactive" yaml:"interactive"`
}

type helpCommand struct {
	Name        string   `json:"name" yaml:"name"`
	Usage       string   `json:"usage" yaml:"usage"`
	Description string   `json:"description" yaml:"description"`
	Examples    []string `json:"examples" yaml:"examples"`
}

func (d *CommandDispatcher) handleHelp(sess ssh.Session, args []string) {
	resp := helpResponse{
		Version:     "0.1",
		Description: "ToolShed — the SSH-native tool registry for AI agents",
		Commands: []helpCommand{
			{
				Name:        "search",
				Usage:       "search <query> [--sort quality|upvotes] [--min-quality N] [--min-upvotes N] [--verified]",
				Description: "Search for tools by name, description, or capability. Filter and sort by reputation.",
				Examples: []string{
					"ssh toolshed.sh search \"fraud detection\"",
					"ssh toolshed.sh search payments --sort quality",
					"ssh toolshed.sh search ml --min-upvotes 3 --min-quality 4",
					"ssh toolshed.sh search api --verified --sort upvotes",
				},
			},
			{
				Name:        "info",
				Usage:       "info <tool_id>",
				Description: "Get full details for a specific tool",
				Examples: []string{
					"ssh toolshed.sh info acme.com/fraud-detection",
				},
			},
			{
				Name:        "report",
				Usage:       "report --tool <id> --latency <ms> --success [--input-hash <hash>] [--output-hash <hash>]",
				Description: "Submit an invocation report",
				Examples: []string{
					"ssh toolshed.sh report --tool acme.com/fraud-detection --latency 120 --success",
					"ssh toolshed.sh report --tool acme.com/fraud-detection --latency 200 --success --input-hash abc123",
				},
			},
			{
				Name:        "upvote",
				Usage:       "upvote <tool_id> --quality <1-5> [--useful] [--invocation <id>] [--comment <text>]",
				Description: "Submit a quality review",
				Examples: []string{
					"ssh toolshed.sh upvote acme.com/fraud-detection --quality 5 --useful --comment \"great\"",
					"ssh toolshed.sh upvote acme.com/fraud-detection --quality 3",
				},
			},
			{
				Name:        "verify",
				Usage:       "verify <domain>",
				Description: "Get DNS verification instructions for domain ownership",
				Examples: []string{
					"ssh toolshed.sh verify acme.com",
				},
			},
			{
				Name:        "crawl",
				Usage:       "crawl <domain>",
				Description: "Index tools from a domain's .well-known/toolshed.yaml",
				Examples: []string{
					"ssh toolshed.sh crawl acme.com",
				},
			},
			{
				Name:        "audit",
				Usage:       "audit <tool_id> [--limit <n>]",
				Description: "View the verifiable Dolt commit history for a tool",
				Examples: []string{
					"ssh toolshed.sh audit acme.com/fraud-detection",
					"ssh toolshed.sh audit acme.com/fraud-detection --limit 50",
				},
			},
			{
				Name:        "reputation",
				Usage:       "reputation <tool_id>",
				Description: "View the computed reputation score for a tool",
				Examples: []string{
					"ssh toolshed.sh reputation acme.com/fraud-detection",
				},
			},
			{
				Name:        "help",
				Usage:       "help",
				Description: "Show available commands and usage",
				Examples: []string{
					"ssh toolshed.sh help",
				},
			},
		},
		Interactive: "Connect without a command for the interactive browser: ssh toolshed.sh",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
	sess.Write([]byte("\n"))
}

// ---------------------------------------------------------------------------
// search <query>
// ---------------------------------------------------------------------------

func (d *CommandDispatcher) handleSearch(sess ssh.Session, args []string) {
	// Parse flags — Go's flag package stops at the first non-flag arg, so
	// we reorder args to move flags before positional query words. This lets
	// users write: search fraud --sort quality --min-upvotes 3
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(sess.Stderr())

	sortBy := fs.String("sort", "", "sort results by: quality, upvotes (default: relevance)")
	minQuality := fs.Float64("min-quality", 0, "minimum average quality score (1-5)")
	minUpvotes := fs.Int("min-upvotes", 0, "minimum number of upvotes")
	verifiedOnly := fs.Bool("verified", false, "only show tools from verified providers")

	if err := fs.Parse(reorderSearchArgs(args)); err != nil {
		return
	}

	queryArgs := fs.Args()
	if len(queryArgs) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: search <query> [--sort quality|upvotes] [--min-quality N] [--min-upvotes N] [--verified]\n")
		fmt.Fprintf(sess.Stderr(), "examples:\n")
		fmt.Fprintf(sess.Stderr(), "  ssh toolshed.sh search \"fraud detection\"\n")
		fmt.Fprintf(sess.Stderr(), "  ssh toolshed.sh search payments --sort quality\n")
		fmt.Fprintf(sess.Stderr(), "  ssh toolshed.sh search ml --min-upvotes 3 --min-quality 4\n")
		fmt.Fprintf(sess.Stderr(), "  ssh toolshed.sh search api --verified --sort upvotes\n")
		return
	}

	query := strings.Join(queryArgs, " ")
	ctx := sess.Context()

	// Try semantic search first if embedder is configured.
	listings, err := d.semanticSearch(ctx, query)
	if err != nil {
		log.Printf("ssh: semantic search %q failed, falling back to LIKE: %v", query, err)
		listings = nil
	}

	// Fall back to LIKE search if semantic search is disabled or returned nothing.
	if len(listings) == 0 {
		listings, err = d.registry.SearchTools(ctx, query)
		if err != nil {
			fmt.Fprintf(sess.Stderr(), "error: search failed: %v\n", err)
			log.Printf("ssh: search %q by %s failed: %v", query, d.fingerprint, err)
			return
		}
	}

	// Enrich each listing with definition + reputation data to build SearchResults.
	results := make([]core.SearchResult, 0, len(listings))
	for _, listing := range listings {
		result := d.buildSearchResult(sess, listing)
		results = append(results, result)
	}

	// ── Filter by reputation criteria ───────────────────────────────────
	results = filterSearchResults(results, *minQuality, *minUpvotes, *verifiedOnly)

	// ── Sort ────────────────────────────────────────────────────────────
	sortSearchResults(results, *sortBy)

	resp := core.SearchResponse{
		Results: results,
		Total:   len(results),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal results: %v\n", err)
		return
	}

	sess.Write(data)
	sess.Write([]byte("\n"))
}

// ---------------------------------------------------------------------------
// Search argument reordering + filtering and sorting helpers
// ---------------------------------------------------------------------------

// reorderSearchArgs moves flag-like arguments before positional (query) words
// so Go's flag.FlagSet parses them correctly. Without this, a command like
// "search fraud --sort quality" would treat "--sort" and "quality" as query
// words because flag.Parse stops at the first non-flag argument ("fraud").
func reorderSearchArgs(args []string) []string {
	valuedFlags := map[string]bool{
		"--sort": true, "-sort": true,
		"--min-quality": true, "-min-quality": true,
		"--min-upvotes": true, "-min-upvotes": true,
	}
	boolFlags := map[string]bool{
		"--verified": true, "-verified": true,
	}

	var flagArgs, queryArgs []string
	for i := 0; i < len(args); i++ {
		if valuedFlags[args[i]] && i+1 < len(args) {
			flagArgs = append(flagArgs, args[i], args[i+1])
			i++ // skip the value
		} else if boolFlags[args[i]] {
			flagArgs = append(flagArgs, args[i])
		} else {
			queryArgs = append(queryArgs, args[i])
		}
	}
	return append(flagArgs, queryArgs...)
}

// filterSearchResults removes results that don't meet the caller's reputation
// criteria. If all thresholds are zero and verifiedOnly is false, it returns
// the slice unchanged (no allocation).
func filterSearchResults(results []core.SearchResult, minQuality float64, minUpvotes int, verifiedOnly bool) []core.SearchResult {
	if minQuality == 0 && minUpvotes == 0 && !verifiedOnly {
		return results
	}

	filtered := make([]core.SearchResult, 0, len(results))
	for _, r := range results {
		if verifiedOnly && !r.Provider.Verified {
			continue
		}
		if minUpvotes > 0 {
			if r.Reputation == nil || r.Reputation.TotalUpvotes < minUpvotes {
				continue
			}
		}
		if minQuality > 0 {
			if r.Reputation == nil || r.Reputation.AvgQuality < minQuality {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// sortSearchResults sorts results in-place by the given criterion.
// Unrecognised values (including "") leave the order unchanged, preserving
// the default (semantic relevance or alphabetical from LIKE search).
func sortSearchResults(results []core.SearchResult, sortBy string) {
	switch sortBy {
	case "quality":
		sort.Slice(results, func(i, j int) bool {
			qi, qj := 0.0, 0.0
			if results[i].Reputation != nil {
				qi = results[i].Reputation.AvgQuality
			}
			if results[j].Reputation != nil {
				qj = results[j].Reputation.AvgQuality
			}
			if qi != qj {
				return qi > qj
			}
			// Tie-break: more upvotes wins.
			ui, uj := 0, 0
			if results[i].Reputation != nil {
				ui = results[i].Reputation.TotalUpvotes
			}
			if results[j].Reputation != nil {
				uj = results[j].Reputation.TotalUpvotes
			}
			return ui > uj
		})
	case "upvotes":
		sort.Slice(results, func(i, j int) bool {
			ui, uj := 0, 0
			if results[i].Reputation != nil {
				ui = results[i].Reputation.TotalUpvotes
			}
			if results[j].Reputation != nil {
				uj = results[j].Reputation.TotalUpvotes
			}
			if ui != uj {
				return ui > uj
			}
			// Tie-break: higher quality wins.
			qi, qj := 0.0, 0.0
			if results[i].Reputation != nil {
				qi = results[i].Reputation.AvgQuality
			}
			if results[j].Reputation != nil {
				qj = results[j].Reputation.AvgQuality
			}
			return qi > qj
		})
	}
}

// semanticSearch performs embedding-based semantic search. Returns nil if
// the embedder is not configured or no embeddings exist yet.
func (d *CommandDispatcher) semanticSearch(ctx context.Context, query string) ([]core.ToolListing, error) {
	if d.embedder == nil {
		return nil, nil
	}

	// Embed the query.
	queryVec, err := d.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Load all tool embeddings from the registry.
	allEmbeddings, err := d.registry.GetAllEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load embeddings: %w", err)
	}
	if len(allEmbeddings) == 0 {
		return nil, nil
	}

	// Rank by cosine similarity with a threshold of 0.3.
	scored := embeddings.RankByCosineSimilarity(queryVec, allEmbeddings, 0.3)
	if len(scored) == 0 {
		return nil, nil
	}

	// Cap at 20 results.
	if len(scored) > 20 {
		scored = scored[:20]
	}

	// Fetch the matching tool listings.
	ids := make([]string, len(scored))
	for i, s := range scored {
		ids[i] = s.ToolID
	}

	listings, err := d.registry.GetToolListingsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("fetch listings: %w", err)
	}

	// Reorder listings to match the score-ranked order.
	listingMap := make(map[string]core.ToolListing, len(listings))
	for _, l := range listings {
		listingMap[l.ID] = l
	}

	ordered := make([]core.ToolListing, 0, len(scored))
	for _, s := range scored {
		if l, ok := listingMap[s.ToolID]; ok {
			ordered = append(ordered, l)
		}
	}

	return ordered, nil
}

// buildSearchResult enriches a ToolListing with its definition and reputation
// to produce a full SearchResult. Errors fetching optional data (definition,
// reputation) are logged but don't prevent the result from being returned.
func (d *CommandDispatcher) buildSearchResult(sess ssh.Session, listing core.ToolListing) core.SearchResult {
	ctx := sess.Context()

	result := core.SearchResult{
		Name:           listing.Name,
		ID:             listing.ID,
		DefinitionHash: listing.DefinitionHash,
		Description:    listing.Description,
		Pricing:        listing.Pricing,
		Payment:        listing.Payment,
		Provider: core.ProviderInfo{
			Domain: listing.ProviderDomain,
		},
	}

	// Fetch the immutable definition for capabilities, schema, and invocation.
	def, err := d.registry.GetToolDefinition(ctx, listing.DefinitionHash)
	if err != nil {
		log.Printf("ssh: failed to get definition %s for %s: %v", listing.DefinitionHash, listing.ID, err)
	}
	if def != nil {
		result.Capabilities = def.Capabilities
		result.Invoke = def.Invocation
		result.Schema = def.Schema
	}

	// Fetch reputation (optional — new tools won't have any).
	rep, err := d.registry.GetReputation(ctx, listing.ID)
	if err != nil {
		log.Printf("ssh: failed to get reputation for %s: %v", listing.ID, err)
	}
	if rep != nil {
		result.Reputation = rep
	}

	// Check domain verification status.
	acct, err := d.registry.GetAccount(ctx, listing.ProviderAccount)
	if err != nil {
		log.Printf("ssh: failed to get provider account for %s: %v", listing.ID, err)
	}
	if acct != nil {
		result.Provider.Verified = acct.DomainVerified
	}

	return result
}

// ---------------------------------------------------------------------------
// info <tool_id>
// ---------------------------------------------------------------------------

// toolInfoResponse is the full detail view returned by the info command.
// It combines the listing, definition, and reputation into a single YAML doc.
type toolInfoResponse struct {
	ID             string            `json:"id" yaml:"id"`
	Name           string            `json:"name" yaml:"name"`
	Description    string            `json:"description,omitempty" yaml:"description,omitempty"`
	VersionLabel   string            `json:"version_label,omitempty" yaml:"version_label,omitempty"`
	DefinitionHash string            `json:"definition_hash" yaml:"definition_hash"`
	Provider       core.ProviderInfo `json:"provider" yaml:"provider"`
	Capabilities   []string          `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Invoke         core.Invocation   `json:"invoke" yaml:"invoke"`
	Schema         core.Schema       `json:"schema" yaml:"schema"`
	Pricing        core.Pricing      `json:"pricing" yaml:"pricing"`
	Payment        core.Payment      `json:"payment,omitempty" yaml:"payment,omitempty"`
	Reputation     *core.Reputation  `json:"reputation,omitempty" yaml:"reputation,omitempty"`
	CreatedAt      time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at" yaml:"updated_at"`
}

func (d *CommandDispatcher) handleInfo(sess ssh.Session, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: info <tool_id>\n")
		fmt.Fprintf(sess.Stderr(), "example: ssh toolshed.sh info acme.com/fraud-detection\n")
		return
	}

	toolID := args[0]
	ctx := sess.Context()

	// 1. Fetch the listing.
	listing, err := d.registry.GetToolListing(ctx, toolID)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to look up tool: %v\n", err)
		log.Printf("ssh: info %q by %s failed: %v", toolID, d.fingerprint, err)
		return
	}
	if listing == nil {
		fmt.Fprintf(sess.Stderr(), "error: tool %q not found\n", toolID)
		return
	}

	resp := toolInfoResponse{
		ID:             listing.ID,
		Name:           listing.Name,
		Description:    listing.Description,
		VersionLabel:   listing.VersionLabel,
		DefinitionHash: listing.DefinitionHash,
		Provider: core.ProviderInfo{
			Domain: listing.ProviderDomain,
		},
		Pricing:   listing.Pricing,
		Payment:   listing.Payment,
		CreatedAt: listing.CreatedAt,
		UpdatedAt: listing.UpdatedAt,
	}

	// 2. Fetch the definition for schema, invocation, capabilities.
	def, err := d.registry.GetToolDefinition(ctx, listing.DefinitionHash)
	if err != nil {
		log.Printf("ssh: info: failed to get definition %s: %v", listing.DefinitionHash, err)
	}
	if def != nil {
		resp.Capabilities = def.Capabilities
		resp.Invoke = def.Invocation
		resp.Schema = def.Schema
	}

	// 3. Fetch reputation.
	rep, err := d.registry.GetReputation(ctx, listing.ID)
	if err != nil {
		log.Printf("ssh: info: failed to get reputation for %s: %v", listing.ID, err)
	}
	if rep != nil {
		resp.Reputation = rep
	}

	// 4. Check provider verification.
	acct, err := d.registry.GetAccount(ctx, listing.ProviderAccount)
	if err != nil {
		log.Printf("ssh: info: failed to get provider account: %v", err)
	}
	if acct != nil {
		resp.Provider.Verified = acct.DomainVerified
	}

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal tool info: %v\n", err)
		return
	}

	sess.Write(data)
	sess.Write([]byte("\n"))
}

// generateEmbedding creates and stores an embedding for a tool. This runs
// asynchronously after registration — failures are logged but don't affect
// the registration response.
func (d *CommandDispatcher) generateEmbedding(listing core.ToolListing, def core.ToolDefinition) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	text := embeddings.BuildToolText(listing.Name, listing.Description, listing.ProviderDomain, def.Capabilities)

	vec, err := d.embedder.Embed(ctx, text)
	if err != nil {
		log.Printf("ssh: failed to generate embedding for %s: %v", listing.ID, err)
		return
	}

	// Compute a hash of the embedded text for staleness detection.
	textHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(text)))

	te := embeddings.ToolEmbedding{
		ToolID:     listing.ID,
		Embedding:  vec,
		Model:      d.embedder.Model(),
		Dimensions: d.embedder.Dimensions(),
		TextHash:   textHash,
	}

	if err := d.registry.StoreEmbedding(ctx, te); err != nil {
		log.Printf("ssh: failed to store embedding for %s: %v", listing.ID, err)
		return
	}

	log.Printf("ssh: generated embedding for %s (%s, %d dims)", listing.ID, d.embedder.Model(), d.embedder.Dimensions())
}

// ---------------------------------------------------------------------------
// report --tool <id> [--definition-hash <hash>] [--latency <ms>]
//        [--success|--failure] [--input-hash <hash>] [--output-hash <hash>]
// ---------------------------------------------------------------------------

// reportResponse is returned after a successful invocation report.
type reportResponse struct {
	InvocationID   string `json:"invocation_id" yaml:"invocation_id"`
	ToolID         string `json:"tool_id" yaml:"tool_id"`
	DefinitionHash string `json:"definition_hash" yaml:"definition_hash"`
	Success        bool   `json:"success" yaml:"success"`
	LedgerCommit   string `json:"ledger_commit" yaml:"ledger_commit"`
	RecordedAt     string `json:"recorded_at" yaml:"recorded_at"`
}

func (d *CommandDispatcher) handleReport(sess ssh.Session, args []string) {
	// ── Rate limit ──────────────────────────────────────────────────────
	// Prevent invocation flooding — a key can only submit N reports per
	// window. This closes the "spam reports to generate upvote slots" hole.
	if d.cmdLimiter != nil && !d.cmdLimiter.Allow(d.fingerprint, "report") {
		fmt.Fprintf(sess.Stderr(), "error: rate limit exceeded — too many reports, try again shortly\n")
		return
	}

	ctx := sess.Context()

	// Parse flags using a custom FlagSet so we don't pollute the global flags.
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(sess.Stderr())

	toolID := fs.String("tool", "", "tool ID (required)")
	defHash := fs.String("definition-hash", "", "definition content hash")
	latency := fs.Int("latency", 0, "invocation latency in milliseconds")
	success := fs.Bool("success", false, "invocation succeeded")
	failure := fs.Bool("failure", false, "invocation failed")
	inputHash := fs.String("input-hash", "", "SHA256 hash of the input")
	outputHash := fs.String("output-hash", "", "SHA256 hash of the output")

	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError means Parse already wrote the error to stderr.
		return
	}

	if *toolID == "" {
		fmt.Fprintf(sess.Stderr(), "error: --tool is required\n")
		fmt.Fprintf(sess.Stderr(), "usage: ssh toolshed.sh report --tool <id> [--latency <ms>] [--success|--failure]\n")
		return
	}

	// Determine success/failure. Default to success if neither flag is set.
	wasSuccess := true
	if *failure {
		wasSuccess = false
	}
	if *success {
		wasSuccess = true
	}

	// If no definition hash provided, look it up from the listing.
	definitionHash := *defHash
	if definitionHash == "" {
		listing, err := d.registry.GetToolListing(ctx, *toolID)
		if err != nil {
			fmt.Fprintf(sess.Stderr(), "error: failed to look up tool: %v\n", err)
			return
		}
		if listing == nil {
			fmt.Fprintf(sess.Stderr(), "error: tool %q not found\n", *toolID)
			return
		}
		definitionHash = listing.DefinitionHash
	}

	now := time.Now().UTC()
	invID := uuid.New().String()

	inv := core.InvocationRecord{
		ID:             invID,
		ToolID:         *toolID,
		DefinitionHash: definitionHash,
		KeyFingerprint: d.fingerprint,
		InputHash:      *inputHash,
		OutputHash:     *outputHash,
		LatencyMs:      *latency,
		Success:        wasSuccess,
		CreatedAt:      now,
	}

	ledgerCommit, err := d.registry.WriteInvocation(ctx, inv)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to write invocation report: %v\n", err)
		log.Printf("ssh: report write failed for %s by %s: %v", *toolID, d.fingerprint, err)
		return
	}

	log.Printf("ssh: invocation %s reported for %s by %s (success=%t, latency=%dms)",
		invID, *toolID, d.fingerprint, wasSuccess, *latency)

	resp := reportResponse{
		InvocationID:   invID,
		ToolID:         *toolID,
		DefinitionHash: definitionHash,
		Success:        wasSuccess,
		LedgerCommit:   ledgerCommit,
		RecordedAt:     now.Format(time.RFC3339),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
	sess.Write([]byte("\n"))
}

// ---------------------------------------------------------------------------
// upvote <tool_id> --quality <1-5> [--useful] [--comment "text"]
// ---------------------------------------------------------------------------

// upvoteResponse is returned after a successful upvote.
type upvoteResponse struct {
	UpvoteID     string `json:"upvote_id" yaml:"upvote_id"`
	ToolID       string `json:"tool_id" yaml:"tool_id"`
	InvocationID string `json:"invocation_id" yaml:"invocation_id"`
	Quality      int    `json:"quality" yaml:"quality"`
	RecordedAt   string `json:"recorded_at" yaml:"recorded_at"`
}

func (d *CommandDispatcher) handleUpvote(sess ssh.Session, args []string) {
	ctx := sess.Context()

	// ── Rate limit ──────────────────────────────────────────────────────
	// Prevent upvote flooding at the command level (complements the
	// per-tool budget check below).
	if d.cmdLimiter != nil && !d.cmdLimiter.Allow(d.fingerprint, "upvote") {
		fmt.Fprintf(sess.Stderr(), "error: rate limit exceeded — too many upvotes, try again shortly\n")
		return
	}

	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: upvote <tool_id> --quality <1-5> [--useful] [--invocation <id>] [--comment \"text\"]\n")
		return
	}

	// The first argument is the tool ID, the rest are flags.
	toolID := args[0]
	flagArgs := args[1:]

	fs := flag.NewFlagSet("upvote", flag.ContinueOnError)
	fs.SetOutput(sess.Stderr())

	quality := fs.Int("quality", 0, "quality score 1-5 (required)")
	useful := fs.Bool("useful", false, "mark as useful")
	invocationID := fs.String("invocation", "", "specific invocation ID to upvote (default: latest)")
	comment := fs.String("comment", "", "optional comment")

	if err := fs.Parse(flagArgs); err != nil {
		return
	}

	if *quality < 1 || *quality > 5 {
		fmt.Fprintf(sess.Stderr(), "error: --quality must be between 1 and 5\n")
		return
	}

	// ── Self-upvote check ───────────────────────────────────────────────
	// A provider cannot upvote their own tool. Compare the upvoter's
	// fingerprint with the tool listing's provider_account.
	listing, err := d.registry.GetToolListing(ctx, toolID)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to look up tool: %v\n", err)
		return
	}
	if listing == nil {
		fmt.Fprintf(sess.Stderr(), "error: tool %q not found\n", toolID)
		return
	}
	if listing.ProviderAccount == d.fingerprint {
		fmt.Fprintf(sess.Stderr(), "error: you cannot upvote your own tool\n")
		return
	}

	// ── Budget check ────────────────────────────────────────────────────
	// Each key can cast at most maxUpvotesPerTool upvotes for a given tool.
	// Each must reference a distinct invocation (enforced by the UNIQUE
	// index on upvotes(key_fingerprint, invocation_id) at the DB level).
	existing, err := d.registry.CountUpvotesByKeyAndTool(ctx, d.fingerprint, toolID)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to check upvote budget: %v\n", err)
		log.Printf("ssh: upvote budget check failed for %s/%s: %v", d.fingerprint, toolID, err)
		return
	}
	if existing >= maxUpvotesPerTool {
		fmt.Fprintf(sess.Stderr(), "error: you have already used all %d upvotes for this tool\n", maxUpvotesPerTool)
		fmt.Fprintf(sess.Stderr(), "hint: each key can upvote a tool up to %d times (once per invocation)\n", maxUpvotesPerTool)
		return
	}

	// Look up the invocation — either a specific one or the most recent.
	var inv *core.InvocationRecord
	if *invocationID != "" {
		inv, err = d.registry.GetInvocationByID(ctx, *invocationID, d.fingerprint)
		if err != nil {
			fmt.Fprintf(sess.Stderr(), "error: failed to look up invocation: %v\n", err)
			log.Printf("ssh: upvote invocation lookup failed for %s: %v", *invocationID, err)
			return
		}
		if inv == nil {
			fmt.Fprintf(sess.Stderr(), "error: invocation %q not found (or doesn't belong to your key)\n", *invocationID)
			return
		}
		if inv.ToolID != toolID {
			fmt.Fprintf(sess.Stderr(), "error: invocation %q is for tool %q, not %q\n", *invocationID, inv.ToolID, toolID)
			return
		}
	} else {
		inv, err = d.registry.GetInvocationByKeyAndTool(ctx, d.fingerprint, toolID)
		if err != nil {
			fmt.Fprintf(sess.Stderr(), "error: failed to check invocation history: %v\n", err)
			log.Printf("ssh: upvote invocation check failed for %s/%s: %v", d.fingerprint, toolID, err)
			return
		}
		if inv == nil {
			fmt.Fprintf(sess.Stderr(), "error: you must report a call before upvoting\n")
			fmt.Fprintf(sess.Stderr(), "hint: use 'report --tool %s --success' to submit an invocation report first\n", toolID)
			return
		}
	}

	now := time.Now().UTC()
	upvoteID := uuid.New().String()

	// Compute a hash of the invocation record for tamper evidence.
	invHashInput := fmt.Sprintf("%s:%s:%s:%t:%d",
		inv.ID, inv.ToolID, inv.KeyFingerprint, inv.Success, inv.CreatedAt.UnixNano())
	sum := sha256.Sum256([]byte(invHashInput))
	invHash := fmt.Sprintf("sha256:%x", sum)

	// Look up the ledger commit hash for the invocation record.
	ledgerCommit, err := d.registry.GetInvocationLedgerCommit(ctx, inv.ID)
	if err != nil {
		log.Printf("ssh: warning: could not look up ledger commit for invocation %s: %v", inv.ID, err)
		// Non-fatal: proceed with empty ledger commit rather than blocking the upvote.
	}

	upvote := core.Upvote{
		ID:             upvoteID,
		ToolID:         toolID,
		KeyFingerprint: d.fingerprint,
		InvocationID:   inv.ID,
		InvocationHash: invHash,
		LedgerCommit:   ledgerCommit,
		QualityScore:   *quality,
		Useful:         *useful,
		Comment:        *comment,
		CreatedAt:      now,
	}

	if err := d.registry.WriteUpvote(ctx, upvote); err != nil {
		if errors.Is(err, dolt.ErrDuplicateUpvote) {
			fmt.Fprintf(sess.Stderr(), "error: you have already upvoted this invocation\n")
			fmt.Fprintf(sess.Stderr(), "hint: use a different --invocation ID, or omit it to use your latest call\n")
			return
		}
		fmt.Fprintf(sess.Stderr(), "error: failed to write upvote: %v\n", err)
		log.Printf("ssh: upvote write failed for %s by %s: %v", toolID, d.fingerprint, err)
		return
	}

	// Recompute reputation for the tool after recording the upvote.
	if err := d.registry.RecomputeReputation(ctx, toolID); err != nil {
		// Non-fatal: log the error but don't fail the upvote.
		log.Printf("ssh: reputation recompute failed for %s: %v", toolID, err)
	}

	log.Printf("ssh: upvote %s for %s by %s (quality=%d)", upvoteID, toolID, d.fingerprint, *quality)

	resp := upvoteResponse{
		UpvoteID:     upvoteID,
		ToolID:       toolID,
		InvocationID: inv.ID,
		Quality:      *quality,
		RecordedAt:   now.Format(time.RFC3339),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
	sess.Write([]byte("\n"))
}

// ---------------------------------------------------------------------------
// audit <tool_id> [--limit <n>]
// ---------------------------------------------------------------------------

// auditResponse is returned by the audit command.
type auditResponse struct {
	ToolID  string            `json:"tool_id" yaml:"tool_id"`
	Entries []dolt.AuditEntry `json:"entries" yaml:"entries"`
	Total   int               `json:"total" yaml:"total"`
}

func (d *CommandDispatcher) handleAudit(sess ssh.Session, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: audit <tool_id> [--limit <n>]\n")
		fmt.Fprintf(sess.Stderr(), "example: ssh toolshed.sh audit acme.com/fraud-detection\n")
		return
	}

	toolID := args[0]
	flagArgs := args[1:]

	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(sess.Stderr())

	limit := fs.Int("limit", 20, "max number of entries to return")

	if err := fs.Parse(flagArgs); err != nil {
		return
	}

	ctx := sess.Context()
	entries, err := d.registry.GetAuditLog(ctx, toolID, *limit)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to query audit log: %v\n", err)
		log.Printf("ssh: audit query failed for %s: %v", toolID, err)
		return
	}

	resp := auditResponse{
		ToolID:  toolID,
		Entries: entries,
		Total:   len(entries),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
	sess.Write([]byte("\n"))
}

// ---------------------------------------------------------------------------
// reputation <tool_id>
// ---------------------------------------------------------------------------

// reputationResponse wraps the reputation data for a tool.
type reputationResponse struct {
	ToolID          string  `json:"tool_id" yaml:"tool_id"`
	TotalUpvotes    int     `json:"total_upvotes" yaml:"total_upvotes"`
	VerifiedUpvotes int     `json:"verified_upvotes" yaml:"verified_upvotes"`
	AvgQuality      float64 `json:"avg_quality" yaml:"avg_quality"`
	UniqueCallers   int     `json:"unique_callers" yaml:"unique_callers"`
	TotalReports    int     `json:"total_reports" yaml:"total_reports"`
	Trend           string  `json:"trend,omitempty" yaml:"trend,omitempty"`
	ComputedAt      string  `json:"computed_at" yaml:"computed_at"`
}

func (d *CommandDispatcher) handleReputation(sess ssh.Session, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: reputation <tool_id>\n")
		fmt.Fprintf(sess.Stderr(), "example: ssh toolshed.sh reputation acme.com/fraud-detection\n")
		return
	}

	toolID := args[0]
	ctx := sess.Context()

	rep, err := d.registry.GetReputation(ctx, toolID)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to look up reputation: %v\n", err)
		log.Printf("ssh: reputation query failed for %s: %v", toolID, err)
		return
	}
	if rep == nil {
		fmt.Fprintf(sess.Stderr(), "error: no reputation data for %q\n", toolID)
		fmt.Fprintf(sess.Stderr(), "hint: reputation is computed after a tool receives its first upvote\n")
		return
	}

	resp := reputationResponse{
		ToolID:          rep.ToolID,
		TotalUpvotes:    rep.TotalUpvotes,
		VerifiedUpvotes: rep.VerifiedUpvotes,
		AvgQuality:      rep.AvgQuality,
		UniqueCallers:   rep.UniqueCallers,
		TotalReports:    rep.TotalReports,
		Trend:           rep.Trend,
		ComputedAt:      rep.ComputedAt.Format(time.RFC3339),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
	sess.Write([]byte("\n"))
}

// ---------------------------------------------------------------------------
// verify <domain>
// ---------------------------------------------------------------------------

// verifyResponse shows DNS TXT record instructions for domain verification.
type verifyResponse struct {
	Domain       string          `json:"domain" yaml:"domain"`
	Fingerprint  string          `json:"fingerprint" yaml:"fingerprint"`
	Status       string          `json:"status" yaml:"status"`
	Instructions verifySteps     `json:"instructions" yaml:"instructions"`
	DNSRecord    verifyDNSRecord `json:"dns_record" yaml:"dns_record"`
}

type verifySteps struct {
	Step1 string `json:"step_1" yaml:"step_1"`
	Step2 string `json:"step_2" yaml:"step_2"`
	Step3 string `json:"step_3" yaml:"step_3"`
}

type verifyDNSRecord struct {
	Type  string `json:"type" yaml:"type"`
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

func (d *CommandDispatcher) handleVerify(sess ssh.Session, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: verify <domain>\n")
		fmt.Fprintf(sess.Stderr(), "example: ssh toolshed.sh verify acme.com\n")
		return
	}

	domain := args[0]
	ctx := sess.Context()

	// Build the expected TXT record value and hostname.
	txtValue := fmt.Sprintf("toolshed-verify=%s", d.fingerprint)
	txtHost := fmt.Sprintf("_toolshed.%s", domain)

	resp := verifyResponse{
		Domain:      domain,
		Fingerprint: d.fingerprint,
		Status:      "pending",
		Instructions: verifySteps{
			Step1: fmt.Sprintf("Add a DNS TXT record to %s", domain),
			Step2: "Wait for DNS propagation (may take up to 48 hours)",
			Step3: fmt.Sprintf("Run: ssh toolshed.sh verify %s", domain),
		},
		DNSRecord: verifyDNSRecord{
			Type:  "TXT",
			Name:  txtHost,
			Value: txtValue,
		},
	}

	// Fast path: already verified in the database.
	acct, err := d.registry.GetAccount(ctx, d.fingerprint)
	if err != nil {
		log.Printf("ssh: verify: failed to get account %s: %v", d.fingerprint, err)
	}
	if acct != nil && acct.DomainVerified && acct.Domain == domain {
		resp.Status = "verified"
		data, _ := json.Marshal(resp)
		sess.Write(data)
		sess.Write([]byte("\n"))
		return
	}

	// Perform DNS TXT lookup to check for the verification record.
	resolver := net.DefaultResolver
	records, err := resolver.LookupTXT(ctx, txtHost)
	if err != nil {
		log.Printf("ssh: verify: DNS lookup for %s failed: %v", txtHost, err)
		// DNS lookup failed — return pending with instructions so the
		// user knows what record to add.
		data, _ := json.Marshal(resp)
		sess.Write(data)
		sess.Write([]byte("\n"))
		return
	}

	// Check if any TXT record matches the expected value.
	matched := false
	for _, r := range records {
		if strings.TrimSpace(r) == txtValue {
			matched = true
			break
		}
	}

	if !matched {
		log.Printf("ssh: verify: no matching TXT record at %s (found %d records)", txtHost, len(records))
		data, _ := json.Marshal(resp)
		sess.Write(data)
		sess.Write([]byte("\n"))
		return
	}

	// DNS record matches — bind the domain to this SSH key.
	if err := d.registry.UpdateAccountDomain(ctx, d.fingerprint, domain); err != nil {
		fmt.Fprintf(sess.Stderr(), "error: DNS record verified but failed to update account: %v\n", err)
		log.Printf("ssh: verify: UpdateAccountDomain failed for %s/%s: %v", d.fingerprint, domain, err)
		return
	}

	log.Printf("ssh: domain %s verified for %s via DNS TXT", domain, d.fingerprint)
	resp.Status = "verified"
	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
	sess.Write([]byte("\n"))
}

// ---------------------------------------------------------------------------
// crawl <domain>
// ---------------------------------------------------------------------------

type crawlResponse struct {
	Domain    string        `json:"domain" yaml:"domain"`
	URL       string        `json:"url" yaml:"url"`
	Tools     []crawledTool `json:"tools" yaml:"tools"`
	Total     int           `json:"total" yaml:"total"`
	CrawledAt string        `json:"crawled_at" yaml:"crawled_at"`
}

type crawledTool struct {
	ID             string `json:"id" yaml:"id"`
	Name           string `json:"name" yaml:"name"`
	DefinitionHash string `json:"definition_hash" yaml:"definition_hash"`
	Status         string `json:"status" yaml:"status"`
}

func (d *CommandDispatcher) handleCrawl(sess ssh.Session, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "error: missing domain argument\n")
		fmt.Fprintf(sess.Stderr(), "usage: ssh toolshed.sh crawl <domain>\n")
		fmt.Fprintf(sess.Stderr(), "example: ssh toolshed.sh crawl acme.com\n")
		return
	}

	domain := strings.TrimSpace(args[0])
	if domain == "" {
		fmt.Fprintf(sess.Stderr(), "error: domain cannot be empty\n")
		return
	}

	// Use a dedicated context with a timeout instead of sess.Context().
	// The SSH session context can be cancelled by wish before the
	// outbound HTTP fetch completes, which silently kills the request
	// and swallows the error output.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("ssh: crawl %s requested by %s", domain, d.fingerprint)
	fmt.Fprintf(sess, "crawling https://%s/.well-known/toolshed.yaml ...\n", domain)

	result, err := crawl.CrawlDomain(ctx, domain, d.registry, d.fingerprint)
	if err != nil {
		fmt.Fprintf(sess, "error: crawl failed: %v\n", err)
		return
	}

	// Convert to YAML-friendly response.
	tools := make([]crawledTool, 0, len(result.Tools))
	for _, t := range result.Tools {
		tools = append(tools, crawledTool{
			ID:             t.ID,
			Name:           t.Name,
			DefinitionHash: t.DefinitionHash,
			Status:         t.Status,
		})
	}

	resp := crawlResponse{
		Domain:    result.Domain,
		URL:       result.URL,
		Tools:     tools,
		Total:     result.Total,
		CrawledAt: result.CrawledAt.Format(time.RFC3339),
	}

	out, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(sess, "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(out)
	sess.Write([]byte("\n"))

	log.Printf("ssh: crawled %s — indexed %d tools", domain, result.Total)
}
