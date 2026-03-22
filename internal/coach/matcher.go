package coach

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

// Suggestion is a coaching recommendation produced by the Matcher.
type Suggestion struct {
	ActionID   string // stable ID for throttle/graduation tracking (e.g., "shell:gs" or "hyprland:movefocus_l")
	Tool       string // tool name (e.g., "zsh", "hyprland")
	UserAction string // what the user typed/did
	Optimal    string // what they should do (alias name or keybind, plus any remaining args)
	SourceFile string // config file path
	SourceLine int    // line number
	KeysSaved  int    // characters saved
}

// matchEntry holds data for a single alias/function/keybind entry.
type matchEntry struct {
	AliasName  string
	Expansion  string
	Tool       string
	SourceFile string
	SourceLine int
}

// Matcher maps normalized command expansions to their shorter aliases.
type Matcher struct {
	// exactMap: normalized expansion -> entry (for exact whole-command matches)
	exactMap map[string]*matchEntry

	// sortedExpansions holds all expansions sorted longest-first for prefix matching.
	// Only entries whose expansion contains a space are included (parameterized aliases).
	sortedExpansions []string
}

// ---------------------------------------------------------------------------
// normalize: canonical form of a shell command string.
// ---------------------------------------------------------------------------

// stripPrefixWords removes a leading zsh/bash meta-word (sudo, noglob, etc.)
// only when it is followed by at least one more word.
func stripPrefixWords(s string) string {
	prefixes := []string{"sudo", "noglob", "nocorrect", "command"}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p+" ") {
			rest := strings.TrimSpace(s[len(p):])
			if rest != "" {
				return rest
			}
		}
	}
	return s
}

// normalize converts a raw shell command to its canonical matchable form.
func normalize(cmd string) string {
	// 1. Trim surrounding whitespace.
	cmd = strings.TrimSpace(cmd)

	// 2. Collapse runs of spaces to a single space.
	cmd = strings.Join(strings.Fields(cmd), " ")

	// 3. Strip leading meta-words (only if more text follows).
	cmd = stripPrefixWords(cmd)

	// 4. Strip trailing pipe chain: find the last unquoted '|' and drop from there.
	if idx := lastUnquotedPipe(cmd); idx >= 0 {
		cmd = strings.TrimSpace(cmd[:idx])
	}

	// 5. Normalize whitespace around && and ; separators.
	cmd = normalizeOperators(cmd, "&&")
	cmd = normalizeOperators(cmd, ";")

	// Re-collapse any spaces introduced by operator normalisation.
	cmd = strings.Join(strings.Fields(cmd), " ")

	return cmd
}

// lastUnquotedPipe returns the index of the last '|' not inside single or double quotes,
// or -1 if none exists.
func lastUnquotedPipe(s string) int {
	inSingle := false
	inDouble := false
	lastPipe := -1
	for i, ch := range s {
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case ch == '|' && !inSingle && !inDouble:
			lastPipe = i
		}
	}
	return lastPipe
}

// normalizeOperators collapses extra spaces around a two-character operator like "&&" or ";".
func normalizeOperators(s, op string) string {
	// Split on the operator, trim each segment, then rejoin.
	parts := strings.Split(s, op)
	if len(parts) == 1 {
		return s
	}
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.Join(parts, " "+op+" ")
}

// ---------------------------------------------------------------------------
// NewMatcher: build the lookup structures from a slice of KBEntry.
// ---------------------------------------------------------------------------

func NewMatcher(entries []kb.KBEntry) *Matcher {
	m := &Matcher{
		exactMap: make(map[string]*matchEntry),
	}

	for i := range entries {
		e := &entries[i]
		// Only process alias, function, and keybind entries.
		if e.Type != parsers.EntryAlias && e.Type != parsers.EntryFunction && e.Type != parsers.EntryKeybind {
			continue
		}
		if e.RawBinding == nil || e.RawAction == nil {
			continue
		}

		aliasName := *e.RawBinding
		expansion := *e.RawAction

		me := &matchEntry{
			AliasName:  aliasName,
			Expansion:  expansion,
			Tool:       e.Tool,
			SourceFile: e.SourceFile,
			SourceLine: e.SourceLine,
		}

		normalizedExpansion := normalize(expansion)
		// Store in exact map (last writer wins on collision, which is fine).
		m.exactMap[normalizedExpansion] = me
	}

	// Build sorted expansion list for prefix matching (longest first).
	for k := range m.exactMap {
		m.sortedExpansions = append(m.sortedExpansions, k)
	}
	sort.Slice(m.sortedExpansions, func(i, j int) bool {
		// Sort longest first so we always find the most-specific prefix.
		return len(m.sortedExpansions[i]) > len(m.sortedExpansions[j])
	})

	return m
}

// ---------------------------------------------------------------------------
// Match: given a source and raw action string, return a Suggestion or nil.
// ---------------------------------------------------------------------------

func (m *Matcher) Match(source string, action string) *Suggestion {
	// For shell sources: normalize the action.
	// For WM keybinds: use as-is.
	normalizedAction := action
	if source == SourceShell {
		normalizedAction = normalize(action)
	}

	// 1. Exact match.
	if me, ok := m.exactMap[normalizedAction]; ok {
		return m.buildSuggestion(me, action, normalizedAction, "")
	}

	// 2. Prefix match: find the longest expansion that is a prefix of the action.
	// We look for expansions that match the beginning of the normalised action
	// followed by a space (indicating remaining arguments).
	for _, exp := range m.sortedExpansions {
		prefix := exp + " "
		if strings.HasPrefix(normalizedAction, prefix) {
			me := m.exactMap[exp]
			remaining := normalizedAction[len(prefix):]
			return m.buildSuggestion(me, action, exp, remaining)
		}
	}

	return nil
}

// buildSuggestion constructs a Suggestion, returning nil if the alias is not shorter.
func (m *Matcher) buildSuggestion(me *matchEntry, userAction, matchedExpansion, remainingArgs string) *Suggestion {
	// Compute the optimal string the user should type instead.
	optimal := me.AliasName
	if remainingArgs != "" {
		optimal = me.AliasName + " " + remainingArgs
	}

	// Only suggest if the alias saves keystrokes.
	if len(optimal) >= len(userAction) {
		return nil
	}

	keysSaved := len(userAction) - len(optimal)

	return &Suggestion{
		ActionID:   fmt.Sprintf("%s:%s", me.Tool, me.AliasName),
		Tool:       me.Tool,
		UserAction: userAction,
		Optimal:    optimal,
		SourceFile: me.SourceFile,
		SourceLine: me.SourceLine,
		KeysSaved:  keysSaved,
	}
}
