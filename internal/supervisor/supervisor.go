package supervisor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/kb"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
	"github.com/shaiknoorullah/wtfrc/internal/session"
)

// recentSessionLimit controls how many sessions a single Review call inspects.
const recentSessionLimit = 20

// Patterns used by the deterministic hallucination checker.
var (
	// modKeyPattern matches keybind references like $mod+Shift+q, $mod+Return, etc.
	modKeyPattern = regexp.MustCompile(`\$mod\+[\w+]+`)

	// backtickPattern extracts backtick-quoted strings that often reference bindings or actions.
	backtickPattern = regexp.MustCompile("`([^`]+)`")
)

// LLMVerification is the structured response from the Tier-2 LLM cross-check.
type LLMVerification struct {
	Accurate         bool     `json:"accurate"`
	HallucinatedRefs []string `json:"hallucinated_refs"`
	Contradictions   []string `json:"contradictions"`
}

// Supervisor audits recent ask sessions for hallucinated or inaccurate answers.
type Supervisor struct {
	db       *kb.DB
	provider llm.Provider
	sessMgr  *session.Manager
}

// New creates a Supervisor.
func New(db *kb.DB, provider llm.Provider, sessMgr *session.Manager) *Supervisor {
	return &Supervisor{
		db:       db,
		provider: provider,
		sessMgr:  sessMgr,
	}
}

// Review runs the full two-tier hallucination-detection pipeline over recent
// sessions and returns a Report. The run is logged to the supervisor_runs table.
func (s *Supervisor) Review(ctx context.Context) (*Report, error) {
	now := time.Now().UTC()

	sessions, err := s.sessMgr.RecentSessions(recentSessionLimit)
	if err != nil {
		return nil, fmt.Errorf("supervisor: list sessions: %w", err)
	}

	report := &Report{
		RunAt:            now,
		SessionsReviewed: len(sessions),
	}

	for _, sess := range sessions {
		full, err := s.sessMgr.GetSession(sess.ID)
		if err != nil {
			return nil, fmt.Errorf("supervisor: get session %s: %w", sess.ID, err)
		}

		for _, q := range full.Queries {
			entries := s.loadEntries(q.EntriesUsed)

			// Tier 1: deterministic checks.
			issues := s.verifyAnswerDeterministic(q, entries)

			// Tier 2: LLM cross-check when deterministic check is inconclusive
			// or entries_used is empty but the answer cites specific values.
			tier := "deterministic"
			if s.needsLLMVerification(q, entries, issues) {
				tier = "llm"
				verification, verErr := s.verifyAnswerLLM(ctx, q, entries)
				if verErr == nil && !verification.Accurate {
					for _, ref := range verification.HallucinatedRefs {
						issues = append(issues, fmt.Sprintf("hallucinated reference: %s", ref))
					}
					for _, c := range verification.Contradictions {
						issues = append(issues, fmt.Sprintf("contradiction: %s", c))
					}
				}
			}

			if len(issues) > 0 {
				report.IssuesFound += len(issues)
				report.FlaggedQueries = append(report.FlaggedQueries, FlaggedQuery{
					QueryID:   q.ID,
					SessionID: q.SessionID,
					Question:  q.Question,
					Issues:    issues,
					Tier:      tier,
				})
			}
		}
	}

	// Generate suggestions based on findings.
	if report.IssuesFound > 0 {
		report.Suggestions = append(report.Suggestions,
			"Re-index source files for entries that could not be found in the KB.",
			"Consider lowering the LLM temperature to reduce hallucination risk.",
		)
	}

	// Log the run to the database.
	if err := s.logRun(report); err != nil {
		return report, fmt.Errorf("supervisor: log run: %w", err)
	}

	return report, nil
}

// loadEntries fetches KB entries by their IDs, silently skipping any that
// cannot be found (which is itself a signal for hallucination).
func (s *Supervisor) loadEntries(ids []int64) []kb.KBEntry {
	var entries []kb.KBEntry
	for _, id := range ids {
		e, err := s.db.GetEntry(id)
		if err == nil && e != nil {
			entries = append(entries, *e)
		}
	}
	return entries
}

// verifyAnswerDeterministic performs Tier-1 checks that need no LLM call.
// It returns a list of issues found (empty means the answer looks OK).
func (s *Supervisor) verifyAnswerDeterministic(q kb.Query, entries []kb.KBEntry) []string {
	var issues []string

	// Check that every entry ID cited actually exists in the DB.
	found := make(map[int64]bool, len(entries))
	for _, e := range entries {
		found[e.ID] = true
	}
	for _, id := range q.EntriesUsed {
		if !found[id] {
			issues = append(issues, fmt.Sprintf("cited entry %d does not exist in the KB", id))
		}
	}

	// Extract keybind / alias patterns from the answer text and cross-check
	// them against the raw_binding and raw_action of cited entries.
	answerRefs := extractAnswerRefs(q.Answer)
	if len(answerRefs) > 0 && len(entries) > 0 {
		entryTexts := collectEntryTexts(entries)
		for _, ref := range answerRefs {
			if !matchesAnyEntry(ref, entryTexts) {
				issues = append(issues, fmt.Sprintf("answer references %q which is not found in cited entries", ref))
			}
		}
	}

	return issues
}

// needsLLMVerification decides whether Tier-2 should run.
func (s *Supervisor) needsLLMVerification(q kb.Query, entries []kb.KBEntry, deterministicIssues []string) bool {
	// If the provider is nil we cannot run the LLM tier.
	if s.provider == nil {
		return false
	}

	// Empty entries_used but the answer mentions specific bindings/values.
	if len(q.EntriesUsed) == 0 {
		refs := extractAnswerRefs(q.Answer)
		if len(refs) > 0 {
			return true
		}
	}

	// Deterministic issues that mention missing references are inconclusive
	// enough to warrant an LLM check.
	for _, iss := range deterministicIssues {
		if strings.Contains(iss, "not found in cited entries") {
			return true
		}
	}

	return false
}

// verifyAnswerLLM performs the Tier-2 LLM cross-check.
func (s *Supervisor) verifyAnswerLLM(ctx context.Context, q kb.Query, entries []kb.KBEntry) (*LLMVerification, error) {
	var entryDescriptions strings.Builder
	for _, e := range entries {
		entryDescriptions.WriteString(fmt.Sprintf("- ID %d: binding=%v action=%v desc=%s\n",
			e.ID, ptrStr(e.RawBinding), ptrStr(e.RawAction), e.Description))
	}

	system := `You are a verification assistant. Given a user question, an answer, and the knowledge-base entries that were cited, determine whether the answer is accurate.
Respond ONLY with JSON matching this schema:
{"accurate": bool, "hallucinated_refs": ["..."], "contradictions": ["..."]}
- accurate: true if the answer faithfully reflects the cited KB entries.
- hallucinated_refs: list any bindings, commands, or values mentioned in the answer that do NOT appear in the cited entries.
- contradictions: list any claims in the answer that directly contradict the cited entries.`

	userMsg := fmt.Sprintf("Question: %s\n\nAnswer: %s\n\nCited KB entries:\n%s",
		q.Question, q.Answer, entryDescriptions.String())

	req := llm.CompletionRequest{
		System: system,
		Messages: []llm.Message{
			{Role: "user", Content: userMsg},
		},
		MaxTokens:   512,
		Temperature: 0.0,
	}

	result, err := llm.CompleteJSON[LLMVerification](ctx, s.provider, req)
	if err != nil {
		return nil, fmt.Errorf("llm verification: %w", err)
	}
	return &result, nil
}

// logRun persists a summary of this supervisor run to the database.
func (s *Supervisor) logRun(r *Report) error {
	modelUsed := ""
	if s.provider != nil {
		modelUsed = s.provider.Name()
	}

	// Gather flagged query IDs as a JSON-ish summary for optimizations_applied.
	var flaggedIDs []string
	for _, fq := range r.FlaggedQueries {
		flaggedIDs = append(flaggedIDs, fmt.Sprintf("q%d", fq.QueryID))
	}
	optSummary := strings.Join(flaggedIDs, ",")

	_, err := s.db.Conn().Exec(
		`INSERT INTO supervisor_runs (run_at, sessions_reviewed, issues_found, optimizations_applied, model_used)
		 VALUES (?, ?, ?, ?, ?)`,
		r.RunAt.Format(time.RFC3339), r.SessionsReviewed, r.IssuesFound, optSummary, modelUsed,
	)
	return err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractAnswerRefs pulls keybind/alias patterns out of an answer string.
func extractAnswerRefs(answer string) []string {
	seen := make(map[string]bool)
	var refs []string

	for _, m := range modKeyPattern.FindAllString(answer, -1) {
		if !seen[m] {
			seen[m] = true
			refs = append(refs, m)
		}
	}

	for _, m := range backtickPattern.FindAllStringSubmatch(answer, -1) {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			refs = append(refs, m[1])
		}
	}

	return refs
}

// collectEntryTexts builds a single string from the raw_binding and raw_action
// fields of every cited entry, for simple substring matching.
func collectEntryTexts(entries []kb.KBEntry) string {
	var parts []string
	for _, e := range entries {
		if e.RawBinding != nil {
			parts = append(parts, *e.RawBinding)
		}
		if e.RawAction != nil {
			parts = append(parts, *e.RawAction)
		}
		parts = append(parts, e.Description)
	}
	return strings.Join(parts, "\n")
}

// matchesAnyEntry checks whether ref appears (case-insensitive) in the
// concatenated entry texts.
func matchesAnyEntry(ref, entryTexts string) bool {
	return strings.Contains(
		strings.ToLower(entryTexts),
		strings.ToLower(ref),
	)
}

// ptrStr safely dereferences a *string for display purposes.
func ptrStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
