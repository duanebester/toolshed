// TUI for the ToolShed SSH server — interactive mode via bubbletea.
//
// When a human connects with `ssh toolshed.sh` (no command), this TUI
// provides an interactive search-and-browse experience for the tool registry.
// It has two views:
//
//  1. Search view — type to search, arrow keys to navigate, enter to select.
//  2. Detail view — full tool info with scrolling, esc to go back.
//
// The TUI is wired up from handleInteractive in server.go.
package ssh

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/toolshed/toolshed/internal/core"
	"github.com/toolshed/toolshed/internal/dolt"
)

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	// Title / header — bold cyan.
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))

	// Search prompt label — bold bright white.
	styleSearchLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))

	// Selected result row — bold cyan to match the cursor indicator.
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))

	// Normal (unselected) result row.
	styleNormal = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	// Provider domain — subtle gray.
	styleDomain = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Verified checkmark — green.
	styleVerified = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))

	// Quality stars — yellow.
	styleStar = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))

	// Pricing text — dim gray.
	stylePricing = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Help bar at the bottom — dark gray.
	styleHelp = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// Section headers in detail view — bold blue.
	styleSection = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))

	// Dim text for secondary information.
	styleDim = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Error messages — red.
	styleError = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	// Label in label: value pairs — dim.
	styleLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Value in label: value pairs — bright white.
	styleValue = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))

	// Back-navigation hint — dark gray.
	styleBackHint = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// searchMsg carries search results (or an error) back from the async search
// goroutine. The id field is used to discard stale results when the user has
// typed additional characters since the search was fired.
type searchMsg struct {
	results []enrichedResult
	err     error
	id      int
}

// doSearchMsg is sent after the debounce delay (300ms) to trigger the actual
// registry search. If the id doesn't match the model's current searchSeqID,
// the message is discarded (the user typed more characters).
type doSearchMsg struct {
	id int
}

// ---------------------------------------------------------------------------
// View state
// ---------------------------------------------------------------------------

type viewState int

const (
	viewSearch viewState = iota
	viewDetail
)

// ---------------------------------------------------------------------------
// Enriched result — SearchResult + display metadata from the listing
// ---------------------------------------------------------------------------

// enrichedResult wraps a core.SearchResult with additional display metadata
// that lives on the ToolListing but not on SearchResult (e.g. version label).
type enrichedResult struct {
	core.SearchResult
	VersionLabel string
}

// ---------------------------------------------------------------------------
// TUIModel
// ---------------------------------------------------------------------------

// TUIModel is the bubbletea model for the interactive ToolShed SSH session.
// It satisfies the tea.Model interface (Init, Update, View).
type TUIModel struct {
	registry    *dolt.Registry
	fingerprint string
	width       int
	height      int

	// Current view (search or detail).
	view viewState

	// --- Search view state ---

	query       string           // current search query text
	results     []enrichedResult // results from the last completed search
	cursor      int              // index of the selected result
	searchSeqID int              // incremented on every keystroke; used for debouncing
	searching   bool             // true while a search request is in-flight
	searchErr   error            // last search error, nil on success

	// --- Detail view state ---

	selected    *enrichedResult // the result being inspected
	scrollY     int             // vertical scroll offset
	detailLines []string        // pre-rendered lines for the detail pane
}

// NewTUIModel creates a new TUI model wired to the given registry.
// width and height should come from the SSH session's PTY dimensions.
func NewTUIModel(registry *dolt.Registry, fingerprint string, width, height int) TUIModel {
	return TUIModel{
		registry:    registry,
		fingerprint: fingerprint,
		width:       width,
		height:      height,
		view:        viewSearch,
	}
}

// ---------------------------------------------------------------------------
// tea.Model interface
// ---------------------------------------------------------------------------

// Init fires an initial empty-query search so the user sees all tools on
// startup without having to type anything.
func (m TUIModel) Init() tea.Cmd {
	return doSearchCmd(m.registry, "", 0)
}

// Update processes incoming messages and returns the updated model.
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Re-render detail lines when the terminal is resized so line
		// wrapping and section dividers adjust to the new width.
		if m.view == viewDetail && m.selected != nil {
			m.detailLines = m.renderDetailContent()
		}
		return m, nil

	case searchMsg:
		// Discard results from a stale search (user kept typing).
		if msg.id != m.searchSeqID {
			return m, nil
		}
		m.searching = false
		if msg.err != nil {
			m.searchErr = msg.err
			return m, nil
		}
		m.searchErr = nil
		m.results = msg.results
		// Clamp the cursor so it doesn't point past the end.
		if m.cursor >= len(m.results) {
			m.cursor = max(0, len(m.results)-1)
		}
		return m, nil

	case doSearchMsg:
		// The debounce timer fired. Only start the search if no newer
		// keystrokes have bumped the sequence ID.
		if msg.id != m.searchSeqID {
			return m, nil
		}
		m.searching = true
		return m, doSearchCmd(m.registry, m.query, m.searchSeqID)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// View renders the TUI to a string.
func (m TUIModel) View() string {
	if m.view == viewDetail {
		return m.viewDetail()
	}
	return m.viewSearch()
}

// ---------------------------------------------------------------------------
// Key handling
// ---------------------------------------------------------------------------

// handleKey is the top-level key dispatcher. Ctrl+C always quits; everything
// else is delegated to the view-specific handler.
func (m TUIModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Ctrl+C is the universal escape hatch.
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.view {
	case viewSearch:
		return m.handleSearchKey(key)
	case viewDetail:
		return m.handleDetailKey(key)
	}
	return m, nil
}

// handleSearchKey processes keys in the search view. The text input is always
// active: printable characters are appended to the query, backspace removes
// the last rune, and arrow keys navigate the result list.
func (m TUIModel) handleSearchKey(key string) (tea.Model, tea.Cmd) {
	switch key {

	// --- Navigation & actions ---

	case "esc", "escape":
		return m, tea.Quit

	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "down":
		if m.cursor < len(m.results)-1 {
			m.cursor++
		}
		return m, nil

	case "enter":
		if len(m.results) > 0 && m.cursor >= 0 && m.cursor < len(m.results) {
			sel := m.results[m.cursor]
			m.selected = &sel
			m.view = viewDetail
			m.scrollY = 0
			m.detailLines = m.renderDetailContent()
		}
		return m, nil

	case "backspace":
		if len(m.query) > 0 {
			runes := []rune(m.query)
			m.query = string(runes[:len(runes)-1])
			m.searchSeqID++
			return m, scheduleSearch(m.searchSeqID)
		}
		return m, nil

	// --- Default: text input ---

	default:
		// "q" quits only when the search input is empty, so users can
		// type queries that contain the letter q.
		if key == "q" && m.query == "" {
			return m, tea.Quit
		}

		// Append printable characters to the query and trigger a
		// debounced search.
		if tuiIsPrintable(key) {
			m.query += key
			m.searchSeqID++
			return m, scheduleSearch(m.searchSeqID)
		}

		return m, nil
	}
}

// handleDetailKey processes keys in the detail view. j/k and arrow keys
// scroll the content; esc goes back to search; q quits.
func (m TUIModel) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {

	case "esc", "escape":
		m.view = viewSearch
		m.selected = nil
		m.scrollY = 0
		m.detailLines = nil
		return m, nil

	case "q":
		return m, tea.Quit

	case "up", "k":
		if m.scrollY > 0 {
			m.scrollY--
		}
		return m, nil

	case "down", "j":
		maxScroll := max(0, len(m.detailLines)-(m.height-2))
		if m.scrollY < maxScroll {
			m.scrollY++
		}
		return m, nil
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Search view rendering
// ---------------------------------------------------------------------------

// viewSearch renders the search view: title, search input, result list,
// status line, and help bar.
func (m TUIModel) viewSearch() string {
	var b strings.Builder

	// ── Title ──
	b.WriteString(" ")
	b.WriteString(styleTitle.Render("🔧 ToolShed"))
	b.WriteString("\n\n")

	// ── Search input with block cursor ──
	b.WriteString(" ")
	b.WriteString(styleSearchLabel.Render("Search: "))
	b.WriteString(m.query)
	b.WriteString("█")
	b.WriteString("\n\n")

	// Compute how many result rows fit on screen.
	// Chrome: title(1) + blank(1) + search(1) + blank(1) + [results] +
	//         blank(1) + status(1) + help(1) = 7 lines.
	maxVisible := m.height - 7
	if maxVisible < 1 {
		maxVisible = 1
	}

	// Scroll window: keep the cursor row always visible.
	offset := 0
	if m.cursor >= maxVisible {
		offset = m.cursor - maxVisible + 1
	}

	// Compute the widest tool name for column alignment.
	maxNameLen := 0
	for _, r := range m.results {
		if n := len(r.Name); n > maxNameLen {
			maxNameLen = n
		}
	}
	// Clamp to reasonable bounds.
	if maxNameLen < 10 {
		maxNameLen = 10
	}
	if maxNameLen > 30 {
		maxNameLen = 30
	}

	// ── Result list ──
	end := min(offset+maxVisible, len(m.results))
	for i := offset; i < end; i++ {
		b.WriteString(m.renderResultLine(m.results[i], i == m.cursor, maxNameLen))
		b.WriteString("\n")
	}

	// Pad remaining lines so the layout stays stable when there are
	// fewer results than available rows.
	for i := end - offset; i < maxVisible; i++ {
		b.WriteString("\n")
	}

	// ── Status line ──
	b.WriteString("\n")
	b.WriteString(" ")
	switch {
	case m.searchErr != nil:
		b.WriteString(styleError.Render(fmt.Sprintf("error: %v", m.searchErr)))
	case m.searching:
		b.WriteString(styleDim.Render("searching…"))
	default:
		b.WriteString(styleDim.Render(fmt.Sprintf("%d tools found", len(m.results))))
	}
	b.WriteString("\n\n")

	// ── Help bar ──
	b.WriteString(" ")
	b.WriteString(styleHelp.Render("↑↓ navigate · enter select · q quit"))

	return b.String()
}

// renderResultLine renders a single row in the search result list.
//
//	▸ Fraud Detection        acme.com ✓  ★ 4.7  free
func (m TUIModel) renderResultLine(r enrichedResult, selected bool, maxNameLen int) string {
	var b strings.Builder

	// Cursor indicator.
	if selected {
		b.WriteString(" ")
		b.WriteString(styleSelected.Render("▸"))
		b.WriteString(" ")
	} else {
		b.WriteString("   ")
	}

	// Tool name, padded for alignment.
	name := r.Name
	if len(name) > maxNameLen {
		name = name[:maxNameLen-1] + "…"
	}
	padded := name + strings.Repeat(" ", max(0, maxNameLen-len(name)))
	if selected {
		b.WriteString(styleSelected.Render(padded))
	} else {
		b.WriteString(styleNormal.Render(padded))
	}

	b.WriteString("  ")

	// Provider domain.
	b.WriteString(styleDomain.Render(r.Provider.Domain))

	// Verified checkmark.
	if r.Provider.Verified {
		b.WriteString(" ")
		b.WriteString(styleVerified.Render("✓"))
	}

	b.WriteString("  ")

	// Quality stars.
	if r.Reputation != nil && r.Reputation.AvgQuality > 0 {
		b.WriteString(styleStar.Render(fmt.Sprintf("★ %.1f", r.Reputation.AvgQuality)))
	} else {
		b.WriteString(styleDim.Render("★ —"))
	}

	b.WriteString("  ")

	// Pricing summary.
	b.WriteString(stylePricing.Render(tuiPricingSummary(r.Pricing)))

	return b.String()
}

// ---------------------------------------------------------------------------
// Detail view rendering
// ---------------------------------------------------------------------------

// viewDetail renders the detail view by slicing the pre-rendered content
// lines according to the current scroll offset and terminal height.
func (m TUIModel) viewDetail() string {
	if m.selected == nil {
		return ""
	}

	// Reserve 2 lines: back hint at top + help bar at bottom.
	availableHeight := m.height - 2
	if availableHeight < 1 {
		availableHeight = 1
	}

	start := min(m.scrollY, len(m.detailLines))
	end := min(start+availableHeight, len(m.detailLines))
	visible := m.detailLines[start:end]

	var b strings.Builder

	// Back hint.
	b.WriteString(" ")
	b.WriteString(styleBackHint.Render("← esc"))
	b.WriteString("\n")

	// Visible content slice.
	for _, line := range visible {
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Pad to fill screen so the help bar sits at the bottom.
	for i := len(visible); i < availableHeight; i++ {
		b.WriteString("\n")
	}

	// Help bar.
	b.WriteString(" ")
	b.WriteString(styleHelp.Render("↑↓ scroll · esc back · q quit"))

	return b.String()
}

// renderDetailContent builds every line of the detail pane. The result is
// stored on the model so we only re-render on resize or selection change,
// not on every scroll.
func (m TUIModel) renderDetailContent() []string {
	r := m.selected
	if r == nil {
		return nil
	}

	var lines []string
	add := func(s string) { lines = append(lines, s) }

	// ── Header ──

	add("") // blank line after the "← esc" hint

	// Tool name + version label (right-aligned when there's room).
	titleStr := " " + styleTitle.Render(r.Name)
	if r.VersionLabel != "" {
		ver := styleDim.Render(r.VersionLabel)
		gap := max(2, m.width-lipgloss.Width(titleStr)-lipgloss.Width(ver)-2)
		titleStr += strings.Repeat(" ", gap) + ver
	}
	add(titleStr)

	// Provider line with optional verified badge.
	providerLine := " " + styleDomain.Render(r.Provider.Domain)
	if r.Provider.Verified {
		providerLine += " " + styleVerified.Render("✓")
	}
	add(providerLine)

	// Description.
	if r.Description != "" {
		add(" " + styleDim.Render(r.Description))
	}

	// ── Invoke section ──

	add("")
	add(" " + tuiSectionHeader("Invoke", m.width-2))
	add(tuiLabelValue("Protocol", r.Invoke.Protocol))
	add(tuiLabelValue("Endpoint", r.Invoke.Endpoint))
	add(tuiLabelValue("Tool Name", r.Invoke.ToolName))

	// ── Schema section ──

	add("")
	add(" " + tuiSectionHeader("Schema", m.width-2))

	if len(r.Schema.Input) > 0 {
		add(" " + styleLabel.Render("Input:"))
		for _, k := range tuiSortedFieldKeys(r.Schema.Input) {
			add(tuiRenderFieldDef(k, r.Schema.Input[k]))
		}
	}
	if len(r.Schema.Output) > 0 {
		add(" " + styleLabel.Render("Output:"))
		for _, k := range tuiSortedFieldKeys(r.Schema.Output) {
			add(tuiRenderFieldDef(k, r.Schema.Output[k]))
		}
	}

	// ── Pricing section ──

	add("")
	add(" " + tuiSectionHeader("Pricing", m.width-2))
	add(tuiLabelValue("Model", tuiPricingSummary(r.Pricing)))
	if r.Pricing.Price > 0 {
		currency := r.Pricing.Currency
		if currency == "" {
			currency = "usd"
		}
		add(tuiLabelValue("Price", fmt.Sprintf("%.4f %s", r.Pricing.Price, currency)))
	}

	// ── Reputation section ──

	add("")
	add(" " + tuiSectionHeader("Reputation", m.width-2))
	if r.Reputation != nil {
		rep := r.Reputation
		var parts []string
		parts = append(parts, styleStar.Render(fmt.Sprintf("Quality ★ %.1f", rep.AvgQuality)))
		parts = append(parts, styleDim.Render(fmt.Sprintf("Upvotes %d", rep.TotalUpvotes)))
		parts = append(parts, styleDim.Render(fmt.Sprintf("Callers %d", rep.UniqueCallers)))
		if rep.TotalReports > 0 {
			parts = append(parts, styleDim.Render(fmt.Sprintf("Reports %d", rep.TotalReports)))
		}
		if rep.Trend != "" {
			parts = append(parts, styleDim.Render(fmt.Sprintf("Trend %s", rep.Trend)))
		}
		add(" " + strings.Join(parts, "   "))
	} else {
		add(" " + styleDim.Render("No reputation data yet"))
	}

	// Trailing blank line for visual breathing room.
	add("")

	return lines
}

// ---------------------------------------------------------------------------
// Async search commands
// ---------------------------------------------------------------------------

// scheduleSearch returns a tea.Cmd that waits 300ms (debounce) then sends
// a doSearchMsg. If the user types another character before the timer fires,
// the new doSearchMsg will have a higher id and this one will be discarded.
func scheduleSearch(id int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(300 * time.Millisecond)
		return doSearchMsg{id: id}
	}
}

// doSearchCmd performs the actual registry search and enriches each listing
// into a full SearchResult with definition details and reputation.
func doSearchCmd(registry *dolt.Registry, query string, id int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		listings, err := registry.SearchTools(ctx, query)
		if err != nil {
			return searchMsg{err: err, id: id}
		}

		results := make([]enrichedResult, 0, len(listings))
		for _, listing := range listings {
			results = append(results, buildResult(registry, ctx, listing))
		}
		return searchMsg{results: results, id: id}
	}
}

// buildResult enriches a ToolListing into a full SearchResult by fetching
// the immutable definition (for schema, invocation, capabilities), the
// reputation aggregate, and the provider's domain verification status.
//
// This is a standalone version of CommandDispatcher.buildSearchResult so
// the TUI doesn't depend on the command dispatch layer.
func buildResult(registry *dolt.Registry, ctx context.Context, listing core.ToolListing) enrichedResult {
	result := enrichedResult{
		SearchResult: core.SearchResult{
			Name:           listing.Name,
			ID:             listing.ID,
			DefinitionHash: listing.DefinitionHash,
			Description:    listing.Description,
			Pricing:        listing.Pricing,
			Payment:        listing.Payment,
			Provider: core.ProviderInfo{
				Domain: listing.ProviderDomain,
			},
		},
		VersionLabel: listing.VersionLabel,
	}

	def, rep, verified := enrichListing(registry, ctx, listing)
	if def != nil {
		result.Capabilities = def.Capabilities
		result.Invoke = def.Invocation
		result.Schema = def.Schema
	}
	if rep != nil {
		result.Reputation = rep
	}
	result.Provider.Verified = verified

	return result
}

// ---------------------------------------------------------------------------
// Rendering helpers
// ---------------------------------------------------------------------------

// tuiSectionHeader renders a section divider like:
//
//	── Invoke ──────────────────────────────────────────
func tuiSectionHeader(title string, width int) string {
	prefix := "── " + title + " "
	remaining := width - len(prefix)
	if remaining < 0 {
		remaining = 0
	}
	return styleSection.Render(prefix + strings.Repeat("─", remaining))
}

// tuiLabelValue renders a "Label    value" pair with consistent indentation
// and a fixed-width label column.
func tuiLabelValue(label, value string) string {
	return " " + styleLabel.Render(fmt.Sprintf("%-12s", label)) + " " + styleValue.Render(value)
}

// tuiRenderFieldDef renders a single schema field definition line:
//
//	transaction_id     string
//	amount             number   min: 0
//	flags              array → string
func tuiRenderFieldDef(name string, fd core.FieldDef) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("   %-20s %s", name, fd.Type))

	if fd.Min != nil {
		b.WriteString(fmt.Sprintf("   min: %g", *fd.Min))
	}
	if fd.Max != nil {
		b.WriteString(fmt.Sprintf("   max: %g", *fd.Max))
	}
	if fd.Items != nil {
		b.WriteString(" → " + fd.Items.Type)
	}

	return " " + styleDim.Render(b.String())
}

// tuiPricingSummary returns a short human-readable pricing string for display
// in search result rows and the detail view.
func tuiPricingSummary(p core.Pricing) string {
	switch p.Model {
	case "free":
		return "free"
	case "per_call":
		if p.Price > 0 {
			return fmt.Sprintf("$%g/call", p.Price)
		}
		return "per call"
	case "subscription":
		if p.Price > 0 {
			return fmt.Sprintf("$%g/mo", p.Price)
		}
		return "subscription"
	case "contact":
		return "contact"
	default:
		if p.Model != "" {
			return p.Model
		}
		return "—"
	}
}

// tuiSortedFieldKeys returns the keys of a FieldDef map in alphabetical order
// so schema fields are rendered deterministically.
func tuiSortedFieldKeys(m map[string]core.FieldDef) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tuiIsPrintable reports whether s represents a single printable rune that
// should be appended to the search query. Multi-character key names like
// "enter" or "backspace" are rejected.
func tuiIsPrintable(s string) bool {
	runes := []rune(s)
	if len(runes) != 1 {
		return false
	}
	return unicode.IsPrint(runes[0])
}
