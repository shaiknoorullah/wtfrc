package supervisor

import (
	"fmt"
	"strings"
	"time"
)

// FlaggedQuery describes a single query that was flagged during review.
type FlaggedQuery struct {
	QueryID   int64
	SessionID string
	Question  string
	Issues    []string
	Tier      string // "deterministic" or "llm"
}

// Report summarises the results of a supervisor review run.
type Report struct {
	RunAt            time.Time
	SessionsReviewed int
	IssuesFound      int
	FlaggedQueries   []FlaggedQuery
	Suggestions      []string
}

// GenerateMarkdown formats the report as a human-readable markdown document.
func GenerateMarkdown(r *Report) string {
	var b strings.Builder

	b.WriteString("# Supervisor Report\n\n")
	b.WriteString(fmt.Sprintf("**Run at:** %s\n\n", r.RunAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("**Sessions reviewed:** %d\n\n", r.SessionsReviewed))
	b.WriteString(fmt.Sprintf("**Issues found:** %d\n\n", r.IssuesFound))

	if len(r.FlaggedQueries) > 0 {
		b.WriteString("## Flagged Queries\n\n")
		for i, fq := range r.FlaggedQueries {
			b.WriteString(fmt.Sprintf("### %d. Query %d (session %s)\n\n", i+1, fq.QueryID, fq.SessionID))
			b.WriteString(fmt.Sprintf("**Question:** %s\n\n", fq.Question))
			b.WriteString(fmt.Sprintf("**Detection tier:** %s\n\n", fq.Tier))
			b.WriteString("**Issues:**\n\n")
			for _, issue := range fq.Issues {
				b.WriteString(fmt.Sprintf("- %s\n", issue))
			}
			b.WriteString("\n")
		}
	}

	if len(r.Suggestions) > 0 {
		b.WriteString("## Suggestions\n\n")
		for _, s := range r.Suggestions {
			b.WriteString(fmt.Sprintf("- %s\n", s))
		}
		b.WriteString("\n")
	}

	return b.String()
}
