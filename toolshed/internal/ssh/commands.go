// Command handlers for the ToolShed SSH server.
//
// Each command is invoked via SSH in non-interactive mode:
//
//	ssh toolshed.sh search "fraud detection"
//	ssh toolshed.sh info acme.com/fraud-detection
//	ssh toolshed.sh register < toolshed.yaml
//	ssh toolshed.sh report --tool acme.com/fraud-detection --latency 120 --success
//	ssh toolshed.sh upvote acme.com/fraud-detection --quality 5 --useful --comment "great"
//	ssh toolshed.sh verify acme.com
//
// All successful output is YAML written to stdout. Errors go to stderr.
package ssh

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/google/uuid"

	"github.com/toolshed/toolshed/internal/core"
	"github.com/toolshed/toolshed/internal/crawl"
	"github.com/toolshed/toolshed/internal/dolt"
	"github.com/toolshed/toolshed/internal/embeddings"
)

// CommandDispatcher routes SSH commands to their handlers.
type CommandDispatcher struct {
	registry    *dolt.Registry
	embedder    embeddings.Embedder // nil = semantic search disabled
	fingerprint string              // SSH key fingerprint of the connected user
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
	case "register":
		d.handleRegister(sess, args)
	case "report":
		d.handleReport(sess, args)
	case "upvote":
		d.handleUpvote(sess, args)
	case "verify":
		d.handleVerify(sess, args)
	case "crawl":
		d.handleCrawl(sess, args)
	case "help":
		d.handleHelp(sess, args)
	default:
		fmt.Fprintf(sess.Stderr(), "error: unknown command %q\n", name)
		fmt.Fprintf(sess.Stderr(), "available commands: search, info, register, report, upvote, verify, crawl, help\n")
	}
}

type helpResponse struct {
	Version     string        `yaml:"version"`
	Description string        `yaml:"description"`
	Commands    []helpCommand `yaml:"commands"`
	Interactive string        `yaml:"interactive"`
}

type helpCommand struct {
	Name        string   `yaml:"name"`
	Usage       string   `yaml:"usage"`
	Description string   `yaml:"description"`
	Examples    []string `yaml:"examples"`
}

func (d *CommandDispatcher) handleHelp(sess ssh.Session, args []string) {
	resp := helpResponse{
		Version:     "0.1",
		Description: "ToolShed — the SSH-native tool registry for AI agents",
		Commands: []helpCommand{
			{
				Name:        "search",
				Usage:       "search <query>",
				Description: "Search for tools by name, description, or capability",
				Examples: []string{
					"ssh toolshed.sh search \"fraud detection\"",
					"ssh toolshed.sh search payments",
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
				Name:        "register",
				Usage:       "register",
				Description: "Register tools from a YAML provider file (pipe to stdin)",
				Examples: []string{
					"ssh toolshed.sh register < toolshed.yaml",
					"cat toolshed.yaml | ssh toolshed.sh register",
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

	data, err := core.MarshalYAML(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
}

// ---------------------------------------------------------------------------
// search <query>
// ---------------------------------------------------------------------------

func (d *CommandDispatcher) handleSearch(sess ssh.Session, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: search <query>\n")
		fmt.Fprintf(sess.Stderr(), "example: ssh toolshed.sh search \"fraud detection\"\n")
		return
	}

	query := strings.Join(args, " ")
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

	resp := core.SearchResponse{
		Results: results,
		Total:   len(results),
	}

	data, err := core.MarshalYAML(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal results: %v\n", err)
		return
	}

	sess.Write(data)
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
	ID             string            `yaml:"id"`
	Name           string            `yaml:"name"`
	Description    string            `yaml:"description,omitempty"`
	VersionLabel   string            `yaml:"version_label,omitempty"`
	DefinitionHash string            `yaml:"definition_hash"`
	Provider       core.ProviderInfo `yaml:"provider"`
	Capabilities   []string          `yaml:"capabilities,omitempty"`
	Invoke         core.Invocation   `yaml:"invoke"`
	Schema         core.Schema       `yaml:"schema"`
	Pricing        core.Pricing      `yaml:"pricing"`
	Payment        core.Payment      `yaml:"payment,omitempty"`
	Reputation     *core.Reputation  `yaml:"reputation,omitempty"`
	CreatedAt      time.Time         `yaml:"created_at"`
	UpdatedAt      time.Time         `yaml:"updated_at"`
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

	data, err := core.MarshalYAML(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal tool info: %v\n", err)
		return
	}

	sess.Write(data)
}

// ---------------------------------------------------------------------------
// register  (reads YAML from stdin)
// ---------------------------------------------------------------------------

// registerResponse is returned after successful tool registration.
type registerResponse struct {
	Registered []registeredTool `yaml:"registered"`
	Total      int              `yaml:"total"`
}

type registeredTool struct {
	ID             string `yaml:"id"`
	Name           string `yaml:"name"`
	DefinitionHash string `yaml:"definition_hash"`
}

func (d *CommandDispatcher) handleRegister(sess ssh.Session, args []string) {
	ctx := sess.Context()

	// Read the YAML provider file from stdin.
	data, err := io.ReadAll(sess)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to read stdin: %v\n", err)
		return
	}

	if len(data) == 0 {
		fmt.Fprintf(sess.Stderr(), "error: no data received on stdin\n")
		fmt.Fprintf(sess.Stderr(), "usage: ssh toolshed.sh register < toolshed.yaml\n")
		return
	}

	// Parse and validate the provider file.
	pf, err := core.ParseProviderFileFromBytes(data)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: invalid provider file: %v\n", err)
		return
	}

	// Convert to definition + listing records.
	defs, listings, err := core.ConvertToRecords(pf, d.fingerprint)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to convert provider file: %v\n", err)
		return
	}

	// Register each definition and listing.
	registered := make([]registeredTool, 0, len(defs))
	for i, def := range defs {
		if err := d.registry.RegisterToolDefinition(ctx, def); err != nil {
			fmt.Fprintf(sess.Stderr(), "error: failed to register definition for %q: %v\n", listings[i].Name, err)
			return
		}

		if err := d.registry.RegisterToolListing(ctx, listings[i]); err != nil {
			fmt.Fprintf(sess.Stderr(), "error: failed to register listing for %q: %v\n", listings[i].Name, err)
			return
		}

		registered = append(registered, registeredTool{
			ID:             listings[i].ID,
			Name:           listings[i].Name,
			DefinitionHash: def.ContentHash,
		})

		log.Printf("ssh: registered %s (hash: %s) by %s", listings[i].ID, def.ContentHash, d.fingerprint)

		// Generate embedding for the newly registered tool (best-effort).
		if d.embedder != nil {
			go d.generateEmbedding(listings[i], def)
		}
	}

	resp := registerResponse{
		Registered: registered,
		Total:      len(registered),
	}

	out, err := core.MarshalYAML(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(out)
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
	InvocationID   string `yaml:"invocation_id"`
	ToolID         string `yaml:"tool_id"`
	DefinitionHash string `yaml:"definition_hash"`
	Success        bool   `yaml:"success"`
	RecordedAt     string `yaml:"recorded_at"`
}

func (d *CommandDispatcher) handleReport(sess ssh.Session, args []string) {
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

	if err := d.registry.WriteInvocation(ctx, inv); err != nil {
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
		RecordedAt:     now.Format(time.RFC3339),
	}

	data, err := core.MarshalYAML(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
}

// ---------------------------------------------------------------------------
// upvote <tool_id> --quality <1-5> [--useful] [--comment "text"]
// ---------------------------------------------------------------------------

// upvoteResponse is returned after a successful upvote.
type upvoteResponse struct {
	UpvoteID     string `yaml:"upvote_id"`
	ToolID       string `yaml:"tool_id"`
	InvocationID string `yaml:"invocation_id"`
	Quality      int    `yaml:"quality"`
	RecordedAt   string `yaml:"recorded_at"`
}

func (d *CommandDispatcher) handleUpvote(sess ssh.Session, args []string) {
	ctx := sess.Context()

	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: upvote <tool_id> --quality <1-5> [--useful] [--comment \"text\"]\n")
		return
	}

	// The first argument is the tool ID, the rest are flags.
	toolID := args[0]
	flagArgs := args[1:]

	fs := flag.NewFlagSet("upvote", flag.ContinueOnError)
	fs.SetOutput(sess.Stderr())

	quality := fs.Int("quality", 0, "quality score 1-5 (required)")
	useful := fs.Bool("useful", false, "mark as useful")
	comment := fs.String("comment", "", "optional comment")

	if err := fs.Parse(flagArgs); err != nil {
		return
	}

	if *quality < 1 || *quality > 5 {
		fmt.Fprintf(sess.Stderr(), "error: --quality must be between 1 and 5\n")
		return
	}

	// Verify that the user has actually called this tool — you can't upvote
	// something you haven't used.
	inv, err := d.registry.GetInvocationByKeyAndTool(ctx, d.fingerprint, toolID)
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

	now := time.Now().UTC()
	upvoteID := uuid.New().String()

	// Compute a hash of the invocation record for tamper evidence.
	invHashInput := fmt.Sprintf("%s:%s:%s:%t:%d",
		inv.ID, inv.ToolID, inv.KeyFingerprint, inv.Success, inv.CreatedAt.UnixNano())
	sum := sha256.Sum256([]byte(invHashInput))
	invHash := fmt.Sprintf("sha256:%x", sum)

	upvote := core.Upvote{
		ID:             upvoteID,
		ToolID:         toolID,
		KeyFingerprint: d.fingerprint,
		InvocationID:   inv.ID,
		InvocationHash: invHash,
		QualityScore:   *quality,
		Useful:         *useful,
		Comment:        *comment,
		CreatedAt:      now,
	}

	if err := d.registry.WriteUpvote(ctx, upvote); err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to write upvote: %v\n", err)
		log.Printf("ssh: upvote write failed for %s by %s: %v", toolID, d.fingerprint, err)
		return
	}

	log.Printf("ssh: upvote %s for %s by %s (quality=%d)", upvoteID, toolID, d.fingerprint, *quality)

	resp := upvoteResponse{
		UpvoteID:     upvoteID,
		ToolID:       toolID,
		InvocationID: inv.ID,
		Quality:      *quality,
		RecordedAt:   now.Format(time.RFC3339),
	}

	data, err := core.MarshalYAML(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
}

// ---------------------------------------------------------------------------
// verify <domain>
// ---------------------------------------------------------------------------

// verifyResponse shows DNS TXT record instructions for domain verification.
type verifyResponse struct {
	Domain       string          `yaml:"domain"`
	Fingerprint  string          `yaml:"fingerprint"`
	Status       string          `yaml:"status"`
	Instructions verifySteps     `yaml:"instructions"`
	DNSRecord    verifyDNSRecord `yaml:"dns_record"`
}

type verifySteps struct {
	Step1 string `yaml:"step_1"`
	Step2 string `yaml:"step_2"`
	Step3 string `yaml:"step_3"`
}

type verifyDNSRecord struct {
	Type  string `yaml:"type"`
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

func (d *CommandDispatcher) handleVerify(sess ssh.Session, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(sess.Stderr(), "usage: verify <domain>\n")
		fmt.Fprintf(sess.Stderr(), "example: ssh toolshed.sh verify acme.com\n")
		return
	}

	domain := args[0]

	// Build the expected TXT record value.
	txtValue := fmt.Sprintf("toolshed-verify=%s", d.fingerprint)

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
			Name:  fmt.Sprintf("_toolshed.%s", domain),
			Value: txtValue,
		},
	}

	// Check if already verified.
	ctx := sess.Context()
	acct, err := d.registry.GetAccount(ctx, d.fingerprint)
	if err != nil {
		log.Printf("ssh: verify: failed to get account %s: %v", d.fingerprint, err)
	}
	if acct != nil && acct.DomainVerified && acct.Domain == domain {
		resp.Status = "verified"
	}

	data, err := core.MarshalYAML(resp)
	if err != nil {
		fmt.Fprintf(sess.Stderr(), "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(data)
}

// ---------------------------------------------------------------------------
// crawl <domain>
// ---------------------------------------------------------------------------

type crawlResponse struct {
	Domain    string        `yaml:"domain"`
	URL       string        `yaml:"url"`
	Tools     []crawledTool `yaml:"tools"`
	Total     int           `yaml:"total"`
	CrawledAt string        `yaml:"crawled_at"`
}

type crawledTool struct {
	ID             string `yaml:"id"`
	Name           string `yaml:"name"`
	DefinitionHash string `yaml:"definition_hash"`
	Status         string `yaml:"status"`
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

	out, err := core.MarshalYAML(resp)
	if err != nil {
		fmt.Fprintf(sess, "error: failed to marshal response: %v\n", err)
		return
	}

	sess.Write(out)

	log.Printf("ssh: crawled %s — indexed %d tools", domain, result.Total)
}
