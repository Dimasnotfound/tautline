package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxReviewBytes = 256 * 1024

type changeFile struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	Added   int    `json:"added,omitempty"`
	Removed int    `json:"removed,omitempty"`
}

type changeStats struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

type changesModelView struct {
	Kind        string       `json:"kind"`
	Title       string       `json:"title"`
	Summary     string       `json:"summary"`
	WorkspaceID string       `json:"workspaceId"`
	Files       []changeFile `json:"files"`
	Stats       changeStats  `json:"stats"`
	Empty       bool         `json:"empty,omitempty"`
	Truncated   bool         `json:"truncated,omitempty"`
}

type changesView struct {
	Kind        string       `json:"kind"`
	Title       string       `json:"title"`
	Summary     string       `json:"summary"`
	WorkspaceID string       `json:"workspaceId"`
	Path        string       `json:"path"`
	Files       []changeFile `json:"files"`
	Diff        string       `json:"diff,omitempty"`
	Stats       changeStats  `json:"stats"`
	Empty       bool         `json:"empty,omitempty"`
	Truncated   bool         `json:"truncated,omitempty"`
}

func (state *workspaceState) buildChangeReview() (changesModelView, changesView, error) {
	state.mu.Lock()
	defer state.mu.Unlock()

	paths := make([]string, 0, len(state.originals))
	for path := range state.originals {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	files := make([]changeFile, 0, len(paths))
	var diffBuilder strings.Builder
	stats := changeStats{}
	truncated := false

	for _, relative := range paths {
		before := state.originals[relative]
		absolute := filepath.Join(state.Root, filepath.FromSlash(relative))
		afterBytes, err := os.ReadFile(absolute)
		afterExists := true
		if err != nil {
			if os.IsNotExist(err) {
				afterExists = false
				afterBytes = nil
			} else {
				return changesModelView{}, changesView{}, err
			}
		}
		if bytes.IndexByte(afterBytes, 0) >= 0 || !utf8.Valid(afterBytes) {
			return changesModelView{}, changesView{}, fmt.Errorf("changed file is binary or not valid UTF-8: %s", relative)
		}
		after := string(afterBytes)
		if before.Exists == afterExists && before.Content == after {
			continue
		}

		status := "modified"
		switch {
		case !before.Exists && afterExists:
			status = "added"
		case before.Exists && !afterExists:
			status = "deleted"
		}
		diff, added, removed, fileTruncated := unifiedDiff(relative, before.Content, after)
		if fileTruncated {
			truncated = true
		}
		if diffBuilder.Len() > 0 && diff != "" {
			diffBuilder.WriteString("\n")
		}
		diffBuilder.WriteString(diff)
		files = append(files, changeFile{Path: relative, Status: status, Added: added, Removed: removed})
		stats.Files++
		stats.Added += added
		stats.Removed += removed
	}

	fullDiff, aggregateTruncated := truncateUTF8(diffBuilder.String(), maxReviewBytes)
	truncated = truncated || aggregateTruncated
	empty := len(files) == 0
	summary := "No pending changes"
	if !empty {
		summary = fmt.Sprintf("%d files · +%d −%d", stats.Files, stats.Added, stats.Removed)
	}
	model := changesModelView{
		Kind:        "show_changes",
		Title:       "Changes",
		Summary:     summary,
		WorkspaceID: state.ID,
		Files:       files,
		Stats:       stats,
		Empty:       empty,
		Truncated:   truncated,
	}
	widget := changesView{
		Kind:        model.Kind,
		Title:       model.Title,
		Summary:     model.Summary,
		WorkspaceID: state.ID,
		Path:        state.Root,
		Files:       files,
		Diff:        fullDiff,
		Stats:       stats,
		Empty:       empty,
		Truncated:   truncated,
	}

	state.originals = make(map[string]fileSnapshot)
	return model, widget, nil
}
