package coach

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/config"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
)

// ---------------------------------------------------------------------------
// Tier 1: static templates
// ---------------------------------------------------------------------------

var templates = map[string]map[string]string{
	"chill": {
		"shell_alias":    "Tip: `{optimal}` does the same thing. ({source_file}:{source_line})",
		"shell_function": "Tip: `{optimal}` does the same thing. ({source_file}:{source_line})",
		"wm_keybind":     "Tip: {optimal} is bound for this. ({source_file}:{source_line})",
		"editor_motion":  "Tip: try {optimal} instead. ({source_file}:{source_line})",
		"default":        "Tip: `{optimal}` is available for this. ({source_file}:{source_line})",
	},
	"moderate": {
		"shell_alias":    "You typed {chars_typed} characters. `{optimal}` saves {keys_saved}. ({source_file}:{source_line})",
		"shell_function": "You typed {chars_typed} characters. `{optimal}` saves {keys_saved}. ({source_file}:{source_line})",
		"wm_keybind":     "{optimal} exists for exactly this. You configured it. ({source_file}:{source_line})",
		"editor_motion":  "{optimal} would have been faster. ({source_file}:{source_line})",
		"default":        "`{optimal}` exists for this. ({source_file}:{source_line})",
	},
	"strict": {
		"shell_alias":    "Nope. You have `{optimal}` for this. Type it. ({source_file}:{source_line})",
		"shell_function": "Nope. You have `{optimal}` for this. Type it. ({source_file}:{source_line})",
		"wm_keybind":     "Use {optimal}. You set it up yourself. ({source_file}:{source_line})",
		"editor_motion":  "Use {optimal}. ({source_file}:{source_line})",
		"default":        "Use `{optimal}`. ({source_file}:{source_line})",
	},
}

// allCategories is the canonical list of coaching message categories.
var allCategories = []string{
	"shell_alias",
	"shell_function",
	"wm_keybind",
	"editor_motion",
	"default",
}

// allModes is the canonical list of coaching modes.
var allModes = []string{"chill", "moderate", "strict"}

// ---------------------------------------------------------------------------
// Roaster
// ---------------------------------------------------------------------------

// Roaster generates coaching messages using a three-tier fallback strategy.
type Roaster struct {
	db  *sql.DB
	llm llm.Provider // may be nil (offline mode)
	cfg *config.CoachConfig
}

// NewRoaster creates a Roaster.
func NewRoaster(db *sql.DB, llmProvider llm.Provider, cfg *config.CoachConfig) *Roaster {
	return &Roaster{
		db:  db,
		llm: llmProvider,
		cfg: cfg,
	}
}

// Generate returns a coaching message for the given suggestion and mode.
//
// Tier 2 (cached pool) is attempted first. If no cached messages exist for the
// category+mode combination the method falls back to Tier 1 (static templates).
func (r *Roaster) Generate(suggestion Suggestion, mode string) string {
	category := categoryFromSuggestion(suggestion)

	// --- Tier 2: cached LLM-generated pool ---
	if r.db != nil {
		if msg := r.tryTier2(category, mode, suggestion); msg != "" {
			return msg
		}
	}

	// --- Tier 1: static templates ---
	return r.tier1(category, mode, suggestion)
}

// tryTier2 looks up the least-used cached message for category+mode, interpolates
// variables, increments its used_count, and returns the result.
// Returns "" when no cached messages are available.
func (r *Roaster) tryTier2(category, mode string, s Suggestion) string {
	var id int64
	var tmpl string

	err := r.db.QueryRow(`
		SELECT id, template
		FROM coaching_messages
		WHERE category = ? AND mode = ?
		ORDER BY used_count ASC, id ASC
		LIMIT 1
	`, category, mode).Scan(&id, &tmpl)

	if err != nil {
		// No rows or query error — fall through to Tier 1.
		return ""
	}

	// Increment used_count.
	_, _ = r.db.Exec(`UPDATE coaching_messages SET used_count = used_count + 1 WHERE id = ?`, id)

	return interpolate(tmpl, s)
}

// tier1 returns a Tier 1 static template message.
func (r *Roaster) tier1(category, mode string, s Suggestion) string {
	modeTemplates, ok := templates[mode]
	if !ok {
		// Unknown mode — fall back to "chill".
		modeTemplates = templates["chill"]
	}
	tmpl, ok := modeTemplates[category]
	if !ok {
		tmpl = modeTemplates["default"]
	}
	return interpolate(tmpl, s)
}

// ---------------------------------------------------------------------------
// RefreshPool — Tier 3 background operation
// ---------------------------------------------------------------------------

// RefreshPool uses the LLM to generate a pool of cached coaching messages.
// It is a no-op when the LLM provider is nil.
// Failures are non-fatal; coaching continues with Tier 1 templates.
func (r *Roaster) RefreshPool(ctx context.Context) error {
	if r.llm == nil {
		return nil
	}

	for _, category := range allCategories {
		for _, mode := range allModes {
			if err := r.refreshCategoryMode(ctx, category, mode); err != nil {
				// Log and continue — do not abort the entire refresh.
				_ = err
			}
		}
	}

	return nil
}

// refreshCategoryMode generates 10 messages for one category+mode pair and
// inserts them into the coaching_messages table.
func (r *Roaster) refreshCategoryMode(ctx context.Context, category, mode string) error {
	prompt := fmt.Sprintf(
		"Generate exactly 10 short coaching messages for a developer power-user tool.\n"+
			"Category: %s\nMode: %s\n\n"+
			"Rules:\n"+
			"- Each message should be concise (under 100 chars).\n"+
			"- Use these placeholders where appropriate: {optimal}, {typed}, {source_file}, {source_line}, {chars_typed}, {keys_saved}.\n"+
			"- Match the tone for mode %q: chill=friendly tip, moderate=matter-of-fact, strict=blunt.\n"+
			"- Return a JSON array of 10 strings. No explanation, no markdown, only the JSON array.\n"+
			"\nExample: [\"Use {optimal} next time.\", ...]",
		category, mode, mode,
	)

	req := llm.CompletionRequest{
		System: "You are a coaching message generator for a terminal efficiency tool. Return only raw JSON.",
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		MaxTokens:      1024,
		Temperature:    0.7,
		ResponseFormat: llm.FormatJSON,
	}

	resp, err := r.llm.Complete(ctx, req)
	if err != nil {
		return fmt.Errorf("refreshCategoryMode %s/%s: llm complete: %w", category, mode, err)
	}

	content := stripFences(resp.Content)

	var messages []string
	if err := json.Unmarshal([]byte(content), &messages); err != nil {
		return fmt.Errorf("refreshCategoryMode %s/%s: parse json: %w", category, mode, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, msg := range messages {
		if msg == "" {
			continue
		}
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO coaching_messages (category, mode, template, variables, generated_at, used_count)
			VALUES (?, ?, ?, ?, ?, 0)
		`, category, mode, msg, "{}", now)
		if err != nil {
			return fmt.Errorf("refreshCategoryMode %s/%s: insert: %w", category, mode, err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// GenerateLive — Tier 3 on-demand
// ---------------------------------------------------------------------------

// GenerateLive calls the LLM with full context to produce a bespoke coaching
// message for complex situations. Returns an error if the LLM is unavailable.
func (r *Roaster) GenerateLive(ctx context.Context, suggestion Suggestion, mode, extraContext string) (string, error) {
	if r.llm == nil {
		return "", fmt.Errorf("GenerateLive: LLM provider is not configured (offline mode)")
	}

	category := categoryFromSuggestion(suggestion)
	contextPart := ""
	if extraContext != "" {
		contextPart = "\nExtra context: " + extraContext
	}

	prompt := fmt.Sprintf(
		"The user typed %q but has a shorter shortcut: %q (defined in %s:%d, saves %d keystrokes).\n"+
			"Category: %s\nCoaching mode: %s%s\n\n"+
			"Write a single, concise coaching message (under 120 chars) in the appropriate tone for mode %q.\n"+
			"Do not include any preamble — just the message.",
		suggestion.UserAction,
		suggestion.Optimal,
		suggestion.SourceFile,
		suggestion.SourceLine,
		suggestion.KeysSaved,
		category,
		mode,
		contextPart,
		mode,
	)

	req := llm.CompletionRequest{
		System: "You are a concise, honest coding coach. Respond with only the coaching message — no preamble.",
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   256,
		Temperature: 0.5,
	}

	resp, err := r.llm.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("GenerateLive: llm complete: %w", err)
	}

	return strings.TrimSpace(resp.Content), nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// interpolate replaces all known placeholders in tmpl with values from s.
func interpolate(tmpl string, s Suggestion) string {
	r := strings.NewReplacer(
		"{optimal}", s.Optimal,
		"{typed}", s.UserAction,
		"{source_file}", s.SourceFile,
		"{source_line}", fmt.Sprintf("%d", s.SourceLine),
		"{chars_typed}", fmt.Sprintf("%d", len(s.UserAction)),
		"{keys_saved}", fmt.Sprintf("%d", s.KeysSaved),
	)
	return r.Replace(tmpl)
}

// categoryFromSuggestion derives the coaching category from a Suggestion's Tool
// and ActionID fields.
func categoryFromSuggestion(s Suggestion) string {
	switch s.Tool {
	case "zsh", "bash":
		return "shell_alias"
	case "hyprland", "tmux", "kitty", "qutebrowser", "yazi":
		return "wm_keybind"
	case "nvim":
		return "editor_motion"
	default:
		return "default"
	}
}

// stripFences removes markdown code fences that LLMs sometimes wrap JSON in.
// Duplicates llm.stripCodeFences to avoid exporting it or creating a circular dep.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i != -1 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i != -1 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}
