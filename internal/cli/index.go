package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/shaiknoorullah/wtfrc/internal/indexer"
	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
)

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index your config files into the knowledge base",
	Long: `Scan configured paths for dotfiles and config files, parse them,
enrich entries via the LLM, and store them in the local knowledge base.

Use --changed to only re-index files that have changed since last run.
Use --status to see what would be indexed without actually indexing.`,
	RunE: runIndex,
}

var (
	indexChanged bool
	indexStatus  bool
)

func init() {
	indexCmd.Flags().BoolVar(&indexChanged, "changed", false, "Only re-index changed files (incremental)")
	indexCmd.Flags().BoolVar(&indexStatus, "status", false, "Dry run: show what would be indexed without indexing")
	rootCmd.AddCommand(indexCmd)
}

func runIndex(cmd *cobra.Command, args []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	scanPaths := d.Cfg.Indexer.ScanPaths
	if d.Cfg.Indexer.AutoDiscover && len(scanPaths) == 0 {
		scanPaths = defaultScanPaths()
	}

	// Expand ~ and glob patterns, then filter to parseable files.
	files, err := expandPaths(scanPaths)
	if err != nil {
		return fmt.Errorf("expand paths: %w", err)
	}

	if indexStatus {
		return showIndexStatus(d, files)
	}

	redact := indexer.NewRedactor(d.Cfg.Privacy.RedactPatterns)
	enricher := indexer.NewLLMEnricher(d.StrongLLM)
	idx := indexer.New(d.DB, enricher, redact)

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	fmt.Println(headerStyle.Render("wtfrc index"))

	if indexChanged {
		fmt.Println("  mode: incremental (changed files only)")
	} else {
		fmt.Println("  mode: full")
	}

	fmt.Printf("  files to process: %d\n\n", len(files))

	if err := idx.Index(context.Background(), files); err != nil {
		return fmt.Errorf("index: %w", err)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	fmt.Println(successStyle.Render("Indexing complete."))
	return nil
}

func showIndexStatus(d *deps, files []string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	changedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
	unchangedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))

	fmt.Println(headerStyle.Render("wtfrc index --status (dry run)"))
	fmt.Println()

	manifest := indexer.NewManifest(d.DB)
	changed := 0
	for _, f := range files {
		isChanged, err := manifest.IsChanged(f)
		if err != nil {
			fmt.Printf("  %s (error: %v)\n", f, err)
			continue
		}
		if isChanged {
			fmt.Println(changedStyle.Render(fmt.Sprintf("  [changed] %s", f)))
			changed++
		} else {
			fmt.Println(unchangedStyle.Render(fmt.Sprintf("  [ok]      %s", f)))
		}
	}

	fmt.Printf("\n  %d of %d files would be re-indexed\n", changed, len(files))
	return nil
}

// defaultScanPaths returns common dotfile locations under $HOME.
func defaultScanPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".config"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".tmux.conf"),
		filepath.Join(home, ".gitconfig"),
		filepath.Join(home, ".ssh", "config"),
		filepath.Join(home, ".vimrc"),
	}
}

// expandPaths resolves ~ prefixes, walks directories, and filters to files
// that have a registered parser.
func expandPaths(paths []string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var result []string
	seen := make(map[string]bool)

	for _, p := range paths {
		if strings.HasPrefix(p, "~/") {
			p = filepath.Join(home, p[2:])
		}

		info, err := os.Stat(p)
		if err != nil {
			continue // skip missing paths
		}

		if !info.IsDir() {
			if !seen[p] && parsers.ForFile(p) != nil {
				seen[p] = true
				result = append(result, p)
			}
			continue
		}

		filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !seen[path] && parsers.ForFile(path) != nil {
				seen[path] = true
				result = append(result, path)
			}
			return nil
		})
	}

	return result, nil
}
