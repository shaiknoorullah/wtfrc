package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

// Manifest tracks file hashes to detect changes between indexing runs.
type Manifest struct {
	db *kb.DB
}

// NewManifest returns a Manifest backed by the given database.
func NewManifest(db *kb.DB) *Manifest {
	return &Manifest{db: db}
}

// ComputeHash returns the SHA-256 hex digest of the file at path.
func (m *Manifest) ComputeHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("compute hash: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("compute hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsChanged reports whether the file at path has changed since the last
// manifest update. It returns true if the hash differs or if the file is
// not yet tracked.
func (m *Manifest) IsChanged(path string) (bool, error) {
	currentHash, err := m.ComputeHash(path)
	if err != nil {
		return false, err
	}

	var storedHash string
	err = m.db.Conn().QueryRow(
		"SELECT sha256 FROM manifest WHERE file_path = ?", path,
	).Scan(&storedHash)
	if err != nil {
		// Not tracked yet — treat as changed.
		return true, nil
	}

	return currentHash != storedHash, nil
}

// Update upserts a manifest entry for the given file.
func (m *Manifest) Update(entry kb.ManifestEntry) error {
	_, err := m.db.Conn().Exec(
		`INSERT INTO manifest (file_path, sha256, mtime, last_indexed, entry_count)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(file_path) DO UPDATE SET
			sha256       = excluded.sha256,
			mtime        = excluded.mtime,
			last_indexed = excluded.last_indexed,
			entry_count  = excluded.entry_count`,
		entry.FilePath,
		entry.SHA256,
		entry.Mtime.Format(time.RFC3339),
		entry.LastIndexed.Format(time.RFC3339),
		entry.EntryCount,
	)
	if err != nil {
		return fmt.Errorf("update manifest: %w", err)
	}
	return nil
}

// Remove deletes the manifest entry for the given file path.
func (m *Manifest) Remove(path string) error {
	_, err := m.db.Conn().Exec("DELETE FROM manifest WHERE file_path = ?", path)
	if err != nil {
		return fmt.Errorf("remove manifest: %w", err)
	}
	return nil
}

// ListTracked returns all manifest entries.
func (m *Manifest) ListTracked() ([]kb.ManifestEntry, error) {
	rows, err := m.db.Conn().Query(
		"SELECT file_path, sha256, mtime, last_indexed, entry_count FROM manifest",
	)
	if err != nil {
		return nil, fmt.Errorf("list tracked: %w", err)
	}
	defer rows.Close()

	var entries []kb.ManifestEntry
	for rows.Next() {
		var e kb.ManifestEntry
		var mtimeStr, lastIndexedStr string
		if err := rows.Scan(&e.FilePath, &e.SHA256, &mtimeStr, &lastIndexedStr, &e.EntryCount); err != nil {
			return nil, fmt.Errorf("scan manifest entry: %w", err)
		}
		e.Mtime, _ = time.Parse(time.RFC3339, mtimeStr)
		e.LastIndexed, _ = time.Parse(time.RFC3339, lastIndexedStr)
		entries = append(entries, e)
	}
	return entries, nil
}
