package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	defaultReadWindowLines    = 200
	maxReadWindowLines        = 2000
	defaultSearchResults      = 20
	maxSearchResults          = 100
	defaultSearchContextLines = 2
	maxSearchContextLines     = 12
	defaultSearchFileBytes    = 4 * 1024 * 1024
	maxSearchFileBytes        = 32 * 1024 * 1024
	defaultSearchOutputBytes  = 64 * 1024
	maxSearchOutputBytes      = 128 * 1024
	maxPreviewLineBytes       = 2048
	defaultInlineCommandBytes = 24 * 1024
	minInlineCommandBytes     = 4 * 1024
	defaultArtifactBytes      = 8 * 1024 * 1024
	maxArtifactBytes          = 64 * 1024 * 1024
	defaultArtifactQuotaBytes = 256 * 1024 * 1024
	minArtifactQuotaBytes     = 16 * 1024 * 1024
)

var (
	artifactIDRE       = regexp.MustCompile(`^art_[a-f0-9]{32}$`)
	importantOutputRE  = regexp.MustCompile(`(?i)(^|[^a-z])(error|fatal|panic|failed|failure|exception|traceback|assertion)([^a-z]|$)`)
	artifactStoreMutex sync.Mutex
)

type sourceProvenance struct {
	SHA256       string `json:"sha256"`
	Bytes        int64  `json:"bytes"`
	Lines        int    `json:"lines"`
	ModifiedUnix int64  `json:"modifiedUnix"`
}

type lineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type searchMatch struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Column     int    `json:"column,omitempty"`
	Excerpt    string `json:"excerpt"`
	SHA256     string `json:"sha256,omitempty"`
	TotalLines int    `json:"totalLines,omitempty"`
}

type searchStats struct {
	FilesScanned   int   `json:"filesScanned"`
	FilesSkipped   int   `json:"filesSkipped,omitempty"`
	FilesTruncated int   `json:"filesTruncated,omitempty"`
	Matches        int   `json:"matches"`
	BytesScanned   int64 `json:"bytesScanned"`
}

type searchView struct {
	Kind        string        `json:"kind"`
	Title       string        `json:"title"`
	Summary     string        `json:"summary"`
	WorkspaceID string        `json:"workspaceId"`
	Path        string        `json:"path"`
	Query       string        `json:"query"`
	Regex       bool          `json:"regex,omitempty"`
	Glob        string        `json:"glob,omitempty"`
	Matches     []searchMatch `json:"matches"`
	Stats       searchStats   `json:"stats"`
	Truncated   bool          `json:"truncated,omitempty"`
}

type artifactReference struct {
	ID               string      `json:"id"`
	SHA256           string      `json:"sha256"`
	MIMEType         string      `json:"mimeType"`
	StoredBytes      int64       `json:"storedBytes"`
	OriginalBytes    int64       `json:"originalBytes"`
	Lines            int         `json:"lines"`
	IncludedRanges   []lineRange `json:"includedRanges,omitempty"`
	OmittedLines     int         `json:"omittedLines,omitempty"`
	CaptureTruncated bool        `json:"captureTruncated,omitempty"`
	Redacted         bool        `json:"redacted,omitempty"`
}

type artifactManifest struct {
	Version          int       `json:"version"`
	ID               string    `json:"id"`
	WorkspaceID      string    `json:"workspaceId"`
	BlobSHA256       string    `json:"blobSha256"`
	MIMEType         string    `json:"mimeType"`
	SourceKind       string    `json:"sourceKind"`
	SourceLabel      string    `json:"sourceLabel"`
	CreatedAt        time.Time `json:"createdAt"`
	StoredBytes      int64     `json:"storedBytes"`
	OriginalBytes    int64     `json:"originalBytes"`
	Lines            int       `json:"lines"`
	CaptureTruncated bool      `json:"captureTruncated,omitempty"`
	Redacted         bool      `json:"redacted,omitempty"`
}

type artifactView struct {
	Kind           string            `json:"kind"`
	Title          string            `json:"title"`
	Summary        string            `json:"summary"`
	WorkspaceID    string            `json:"workspaceId"`
	Artifact       artifactReference `json:"artifact"`
	SourceKind     string            `json:"sourceKind"`
	SourceLabel    string            `json:"sourceLabel"`
	Content        string            `json:"content,omitempty"`
	Matches        []searchMatch     `json:"matches,omitempty"`
	StartLine      int               `json:"startLine,omitempty"`
	EndLine        int               `json:"endLine,omitempty"`
	TotalLines     int               `json:"totalLines"`
	NextCursor     string            `json:"nextCursor,omitempty"`
	PreviousCursor string            `json:"previousCursor,omitempty"`
	Truncated      bool              `json:"truncated,omitempty"`
	Stats          toolStats         `json:"stats"`
}

type numberedLine struct {
	Number int
	Text   string
}

type lineWindow struct {
	Content         string
	StartLine       int
	EndLine         int
	TotalLines      int
	NextCursor      string
	PreviousCursor  string
	OutputTruncated bool
	Provenance      sourceProvenance
}

type commandCaptureResult struct {
	Output             string
	Artifact           *artifactReference
	OriginalBytes      int64
	StoredBytes        int64
	TotalLines         int
	IncludedRanges     []lineRange
	OmittedLines       int
	CaptureTruncated   bool
	Redacted           bool
	StorageUnavailable bool
}

type boundedCaptureWriter struct {
	mu        sync.Mutex
	file      *os.File
	limit     int64
	stored    int64
	total     int64
	truncated bool
}

func (writer *boundedCaptureWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	originalLength := len(data)
	writer.total += int64(originalLength)
	remaining := writer.limit - writer.stored
	if remaining <= 0 {
		writer.truncated = true
		return originalLength, nil
	}
	toWrite := data
	if int64(len(toWrite)) > remaining {
		toWrite = toWrite[:remaining]
		writer.truncated = true
	}
	written, err := writer.file.Write(toWrite)
	writer.stored += int64(written)
	if err != nil {
		return 0, err
	}
	return originalLength, nil
}

func (writer *boundedCaptureWriter) snapshot() (stored, total int64, truncated bool) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.stored, writer.total, writer.truncated
}

type previewCollector struct {
	headLimit      int
	tailLimit      int
	beforeLimit    int
	afterLimit     int
	recent         []numberedLine
	tail           []numberedLine
	selected       map[int]string
	afterUntilLine int
}

func newPreviewCollector() *previewCollector {
	return &previewCollector{
		headLimit:   40,
		tailLimit:   40,
		beforeLimit: 3,
		afterLimit:  6,
		selected:    make(map[int]string),
	}
}

func (collector *previewCollector) add(line numberedLine) {
	important := importantOutputRE.MatchString(line.Text)
	displayLine := line
	displayLine.Text = compactPreviewLine(line.Text, maxPreviewLineBytes)
	if line.Number <= collector.headLimit {
		collector.selected[line.Number] = displayLine.Text
	}
	if important {
		for _, previous := range collector.recent {
			collector.selected[previous.Number] = previous.Text
		}
		collector.selected[line.Number] = displayLine.Text
		if until := line.Number + collector.afterLimit; until > collector.afterUntilLine {
			collector.afterUntilLine = until
		}
	} else if line.Number <= collector.afterUntilLine {
		collector.selected[line.Number] = displayLine.Text
	}

	collector.recent = append(collector.recent, displayLine)
	if len(collector.recent) > collector.beforeLimit {
		collector.recent = collector.recent[len(collector.recent)-collector.beforeLimit:]
	}
	collector.tail = append(collector.tail, displayLine)
	if len(collector.tail) > collector.tailLimit {
		collector.tail = collector.tail[len(collector.tail)-collector.tailLimit:]
	}
}

func compactPreviewLine(value string, limit int) string {
	if limit < 128 || len(value) <= limit {
		return value
	}
	headLimit := limit / 2
	tailLimit := limit - headLimit
	head := truncateUTF8Prefix(value, headLimit)
	tail := truncateUTF8Suffix(value, tailLimit)
	omitted := len(value) - len(head) - len(tail)
	return fmt.Sprintf("%s ... %d bytes omitted within line ... %s", head, omitted, tail)
}

func truncateUTF8Prefix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	data := []byte(value)[:limit]
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data)
}

func truncateUTF8Suffix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	data := []byte(value)
	start := len(data) - limit
	for start < len(data) && !utf8.Valid(data[start:]) {
		start++
	}
	return string(data[start:])
}

func appendStringBounded(buffer *bytes.Buffer, value string, limit int64) {
	remaining := limit + 1 - int64(buffer.Len())
	if remaining <= 0 {
		return
	}
	if int64(len(value)) <= remaining {
		_, _ = buffer.WriteString(value)
		return
	}
	_, _ = buffer.WriteString(truncateUTF8Prefix(value, int(remaining)))
}

func contextOutputByteLimit() int {
	legacyFallback := envInt("DEVSPACE_SEARCH_OUTPUT_BYTES", defaultSearchOutputBytes)
	return clampInt(envInt("DEVSPACE_CONTEXT_OUTPUT_BYTES", legacyFallback), 8*1024, maxSearchOutputBytes)
}

func formatNumberedLine(width int, line numberedLine) string {
	return fmt.Sprintf("%*d  %s", width, line.Number, compactPreviewLine(line.Text, maxPreviewLineBytes))
}

func selectNumberedLinesWithinBudget(lines []numberedLine, width, budget int, fromEnd bool) []numberedLine {
	if len(lines) == 0 || budget <= 0 {
		return nil
	}
	selected := make([]numberedLine, 0, len(lines))
	used := 0
	add := func(line numberedLine) bool {
		line.Text = compactPreviewLine(line.Text, maxPreviewLineBytes)
		cost := len(formatNumberedLine(width, line))
		if len(selected) > 0 {
			cost++
		}
		if used+cost > budget {
			return false
		}
		selected = append(selected, line)
		used += cost
		return true
	}
	if fromEnd {
		for index := len(lines) - 1; index >= 0; index-- {
			if !add(lines[index]) {
				break
			}
		}
		for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
			selected[left], selected[right] = selected[right], selected[left]
		}
		return selected
	}
	for _, line := range lines {
		if !add(line) {
			break
		}
	}
	return selected
}

func renderNumberedLines(lines []numberedLine, totalLines int) (string, []lineRange) {
	if len(lines) == 0 {
		return "", nil
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Number < lines[j].Number })
	width := len(strconv.Itoa(maxInt(totalLines, 1)))
	var builder strings.Builder
	var ranges []lineRange
	current := lineRange{Start: lines[0].Number}
	previous := 0
	for index, line := range lines {
		if index > 0 && line.Number != previous+1 {
			current.End = previous
			ranges = append(ranges, current)
			fmt.Fprintf(&builder, "\n... %d lines omitted ...\n", line.Number-previous-1)
			current = lineRange{Start: line.Number}
		}
		builder.WriteString(formatNumberedLine(width, line))
		if index < len(lines)-1 {
			builder.WriteByte('\n')
		}
		previous = line.Number
	}
	current.End = previous
	ranges = append(ranges, current)
	return builder.String(), ranges
}

func (collector *previewCollector) finish(totalLines, byteLimit int) (string, []lineRange, int) {
	for _, line := range collector.tail {
		collector.selected[line.Number] = line.Text
	}
	all := make([]numberedLine, 0, len(collector.selected))
	for lineNumber, text := range collector.selected {
		all = append(all, numberedLine{Number: lineNumber, Text: text})
	}
	if len(all) == 0 {
		return "", nil, totalLines
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Number < all[j].Number })
	full, fullRanges := renderNumberedLines(append([]numberedLine(nil), all...), totalLines)
	if byteLimit <= 0 || len(full) <= byteLimit {
		omitted := maxInt(0, totalLines-len(all))
		return full, fullRanges, omitted
	}

	tailNumbers := make(map[int]bool, len(collector.tail))
	for _, line := range collector.tail {
		tailNumbers[line.Number] = true
	}
	var headLines, middleLines, tailLines []numberedLine
	for _, line := range all {
		switch {
		case line.Number <= collector.headLimit:
			headLines = append(headLines, line)
		case tailNumbers[line.Number]:
			tailLines = append(tailLines, line)
		default:
			middleLines = append(middleLines, line)
		}
	}
	if len(middleLines) > 40 {
		middleLines = append(append([]numberedLine(nil), middleLines[:20]...), middleLines[len(middleLines)-20:]...)
	}

	reserve := minInt(4096, byteLimit/4)
	usable := maxInt(1024, byteLimit-reserve)
	weight := 0
	if len(headLines) > 0 {
		weight++
	}
	if len(middleLines) > 0 {
		weight += 2
	}
	if len(tailLines) > 0 {
		weight++
	}
	if weight == 0 {
		weight = 1
	}
	unit := usable / weight
	width := len(strconv.Itoa(maxInt(totalLines, 1)))
	selectedByNumber := map[int]numberedLine{}
	addSelected := func(lines []numberedLine) {
		for _, line := range lines {
			selectedByNumber[line.Number] = line
		}
	}
	if len(headLines) > 0 {
		addSelected(selectNumberedLinesWithinBudget(headLines, width, unit, false))
	}
	if len(middleLines) > 0 {
		addSelected(selectNumberedLinesWithinBudget(middleLines, width, unit*2, false))
	}
	if len(tailLines) > 0 {
		addSelected(selectNumberedLinesWithinBudget(tailLines, width, unit, true))
	}
	selected := make([]numberedLine, 0, len(selectedByNumber))
	for _, line := range selectedByNumber {
		selected = append(selected, line)
	}
	preview, ranges := renderNumberedLines(selected, totalLines)
	if len(preview) > byteLimit {
		preview = truncateUTF8Prefix(preview, byteLimit)
	}
	omitted := maxInt(0, totalLines-len(selected))
	return preview, ranges, omitted
}

func handleWorkspaceSearch(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspaceID := argStr(req, "workspace_id")
	state, err := getWorkspace(workspaceID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := argStr(req, "query")
	if strings.TrimSpace(query) == "" {
		return mcp.NewToolResultError("query must not be empty"), nil
	}
	useRegex := argBool(req, "regex", false)
	caseSensitive := argBool(req, "case_sensitive", false)
	glob := strings.TrimSpace(argStr(req, "glob"))
	includeHidden := argBool(req, "include_hidden", false)
	contextLines := clampInt(argInt(req, "context_lines", defaultSearchContextLines), 0, maxSearchContextLines)
	maxResults := clampInt(argInt(req, "max_results", defaultSearchResults), 1, maxSearchResults)
	maxFileBytes := clampInt(envInt("DEVSPACE_SEARCH_FILE_BYTES", defaultSearchFileBytes), 64*1024, maxSearchFileBytes)
	maxOutputBytes := contextOutputByteLimit()

	matcher, err := compileSearchMatcher(query, useRegex, caseSensitive)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	matches := make([]searchMatch, 0, maxResults)
	stats := searchStats{}
	truncated := false
	outputBytes := 0

	walkErr := filepath.WalkDir(state.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			stats.FilesSkipped++
			return nil
		}
		if path == state.Root {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if shouldSkipDir(name) || (!includeHidden && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !includeHidden && strings.HasPrefix(name, ".") {
			stats.FilesSkipped++
			return nil
		}
		relative, relErr := state.relativePath(path)
		if relErr != nil || !searchGlobMatches(glob, relative) {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			stats.FilesSkipped++
			return nil
		}
		limit := int64(maxFileBytes)
		file, openErr := os.Open(path)
		if openErr != nil {
			stats.FilesSkipped++
			return nil
		}
		data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
		_ = file.Close()
		if readErr != nil {
			stats.FilesSkipped++
			return nil
		}
		fileTruncated := int64(len(data)) > limit || info.Size() > limit
		if int64(len(data)) > limit {
			data = data[:limit]
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			stats.FilesSkipped++
			return nil
		}
		stats.FilesScanned++
		stats.BytesScanned += int64(len(data))
		if fileTruncated {
			stats.FilesTruncated++
		}
		provenance := provenanceForBytes(data, info)
		fullProvenanceLoaded := !fileTruncated
		lines := strings.Split(string(data), "\n")
		for lineIndex, line := range lines {
			column := matcher(line)
			if column < 0 {
				continue
			}
			if !fullProvenanceLoaded {
				fullProvenance, provenanceErr := provenanceForFile(path)
				if provenanceErr != nil {
					stats.FilesSkipped++
					return nil
				}
				provenance = fullProvenance
				fullProvenanceLoaded = true
			}
			start := maxInt(0, lineIndex-contextLines)
			end := minInt(len(lines)-1, lineIndex+contextLines)
			excerpt := boundedNumberedExcerpt(lines, start, end)
			if outputBytes+len(excerpt) > maxOutputBytes {
				truncated = true
				return filepath.SkipAll
			}
			outputBytes += len(excerpt)
			matches = append(matches, searchMatch{
				Path:       relative,
				Line:       lineIndex + 1,
				Column:     column + 1,
				Excerpt:    excerpt,
				SHA256:     provenance.SHA256,
				TotalLines: provenance.Lines,
			})
			stats.Matches++
			if len(matches) >= maxResults {
				truncated = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return mcp.NewToolResultError(walkErr.Error()), nil
	}

	view := searchView{
		Kind:        "search",
		Title:       "Workspace search",
		Summary:     fmt.Sprintf("%d matches in %d files", len(matches), stats.FilesScanned),
		WorkspaceID: workspaceID,
		Path:        state.Root,
		Query:       query,
		Regex:       useRegex,
		Glob:        glob,
		Matches:     matches,
		Stats:       stats,
		Truncated:   truncated,
	}
	fallback := fmt.Sprintf("Found %d matches in %d files.", len(matches), stats.FilesScanned)
	if truncated {
		fallback += " Results limited."
	}
	return newToolResult("search", view, view, fallback), nil
}

func compileSearchMatcher(query string, useRegex, caseSensitive bool) (func(string) int, error) {
	if useRegex {
		pattern := query
		if !caseSensitive {
			pattern = "(?i)" + pattern
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		return func(line string) int {
			location := compiled.FindStringIndex(line)
			if location == nil {
				return -1
			}
			return utf8.RuneCountInString(line[:location[0]])
		}, nil
	}
	needle := query
	if !caseSensitive {
		needle = strings.ToLower(needle)
	}
	return func(line string) int {
		candidate := line
		if !caseSensitive {
			candidate = strings.ToLower(candidate)
		}
		byteIndex := strings.Index(candidate, needle)
		if byteIndex < 0 {
			return -1
		}
		return utf8.RuneCountInString(candidate[:byteIndex])
	}, nil
}

func searchGlobMatches(pattern, relative string) bool {
	if strings.TrimSpace(pattern) == "" {
		return true
	}
	relative = filepath.ToSlash(relative)
	for _, candidate := range strings.FieldsFunc(pattern, func(r rune) bool { return r == ',' || r == ';' }) {
		candidate = strings.TrimSpace(filepath.ToSlash(candidate))
		if candidate == "" {
			continue
		}
		if matched, _ := filepath.Match(filepath.FromSlash(candidate), filepath.FromSlash(relative)); matched {
			return true
		}
		if matched, _ := filepath.Match(filepath.FromSlash(candidate), filepath.Base(relative)); matched {
			return true
		}
	}
	return false
}

func readWorkspaceFileWindow(req mcp.CallToolRequest) (fileView, error) {
	workspaceID := argStr(req, "workspace_id")
	_, path, err := resolveWorkspacePath(workspaceID, argStr(req, "path"))
	if err != nil {
		return fileView{}, err
	}
	maxBytes := clampInt(argInt(req, "max_bytes", defaultReadBytes), 1024, maxReadBytes)
	startLine := argInt(req, "start_line", 0)
	endLine := argInt(req, "end_line", 0)
	head := argInt(req, "head", 0)
	tail := argInt(req, "tail", 0)
	maxLines := clampInt(argInt(req, "max_lines", defaultReadWindowLines), 1, maxReadWindowLines)
	cursor := strings.TrimSpace(argStr(req, "cursor"))
	expectedSHA := strings.ToLower(strings.TrimSpace(argStr(req, "expected_sha256")))

	windowRequested := startLine > 0 || endLine > 0 || head > 0 || tail > 0 || cursor != "" || argInt(req, "max_lines", 0) > 0
	if !windowRequested {
		content, truncated, info, readErr := readTextBounded(path, maxBytes)
		if readErr != nil {
			return fileView{}, readErr
		}
		provenance, metadataErr := provenanceForFile(path)
		if metadataErr != nil {
			return fileView{}, metadataErr
		}
		if expectedSHA != "" && !hashMatchesExpected(provenance.SHA256, expectedSHA) {
			return fileView{}, fmt.Errorf("file changed: expected sha256 %s, current sha256 %s", expectedSHA, provenance.SHA256)
		}
		shownLines := countLines(content)
		end := shownLines
		next := ""
		if truncated && shownLines < provenance.Lines {
			next = makeLineCursor(shownLines+1, provenance.SHA256)
		}
		relative := filepath.ToSlash(argStr(req, "path"))
		return fileView{
			Kind:         "file",
			Title:        filepath.Base(path),
			Summary:      fmt.Sprintf("%d of %d lines · %d bytes shown", shownLines, provenance.Lines, len(content)),
			WorkspaceID:  workspaceID,
			Path:         relative,
			Language:     languageFromPath(path),
			Content:      withLineNumbersFrom(content, 1),
			Stats:        toolStats{Bytes: info.Size(), Lines: shownLines},
			Truncated:    truncated,
			SHA256:       provenance.SHA256,
			StartLine:    boolInt(shownLines > 0, 1),
			EndLine:      end,
			TotalLines:   provenance.Lines,
			NextCursor:   next,
			ModifiedUnix: provenance.ModifiedUnix,
		}, nil
	}

	if cursor != "" {
		cursorLine, cursorSHA, cursorErr := parseLineCursor(cursor)
		if cursorErr != nil {
			return fileView{}, cursorErr
		}
		if startLine == 0 {
			startLine = cursorLine
		}
		if expectedSHA == "" && cursorSHA != "" {
			expectedSHA = cursorSHA
		}
	}
	if head > 0 && tail > 0 {
		return fileView{}, errors.New("head and tail cannot be used together")
	}
	if head > 0 {
		startLine = 1
		endLine = head
	}
	if tail == 0 {
		if startLine <= 0 {
			startLine = 1
		}
		if endLine <= 0 {
			endLine = startLine + maxLines - 1
		}
		if endLine < startLine {
			return fileView{}, errors.New("end_line must be greater than or equal to start_line")
		}
		if endLine-startLine+1 > maxReadWindowLines {
			endLine = startLine + maxReadWindowLines - 1
		}
	} else {
		tail = clampInt(tail, 1, maxReadWindowLines)
	}

	window, err := readLineWindow(path, startLine, endLine, tail)
	if err != nil {
		return fileView{}, err
	}
	if expectedSHA != "" && !hashMatchesExpected(window.Provenance.SHA256, expectedSHA) {
		return fileView{}, fmt.Errorf("file changed: expected sha256 %s, current sha256 %s", expectedSHA, window.Provenance.SHA256)
	}
	relative := filepath.ToSlash(argStr(req, "path"))
	shownLines := 0
	if window.StartLine > 0 && window.EndLine >= window.StartLine {
		shownLines = window.EndLine - window.StartLine + 1
	}
	return fileView{
		Kind:           "file",
		Title:          filepath.Base(path),
		Summary:        fmt.Sprintf("lines %d-%d of %d", window.StartLine, window.EndLine, window.TotalLines),
		WorkspaceID:    workspaceID,
		Path:           relative,
		Language:       languageFromPath(path),
		Content:        window.Content,
		Stats:          toolStats{Bytes: window.Provenance.Bytes, Lines: shownLines},
		Truncated:      window.StartLine > 1 || window.EndLine < window.TotalLines,
		SHA256:         window.Provenance.SHA256,
		StartLine:      window.StartLine,
		EndLine:        window.EndLine,
		TotalLines:     window.TotalLines,
		NextCursor:     window.NextCursor,
		PreviousCursor: window.PreviousCursor,
		ModifiedUnix:   window.Provenance.ModifiedUnix,
	}, nil
}

func readLineWindow(path string, requestedStart, requestedEnd, tailCount int) (lineWindow, error) {
	file, err := os.Open(path)
	if err != nil {
		return lineWindow{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return lineWindow{}, err
	}
	if info.IsDir() {
		return lineWindow{}, fmt.Errorf("not a file: %s", path)
	}

	hasher := sha256.New()
	reader := bufio.NewReaderSize(io.TeeReader(file, hasher), 64*1024)
	selected := make([]numberedLine, 0, maxInt(1, requestedEnd-requestedStart+1))
	tail := make([]numberedLine, 0, tailCount)
	lineNumber := 0
	lastEndedWithNewline := false
	for {
		data, readErr := reader.ReadString('\n')
		if len(data) > 0 {
			if strings.IndexByte(data, 0) >= 0 || !utf8.ValidString(data) {
				return lineWindow{}, fmt.Errorf("file is binary or not valid UTF-8: %s", path)
			}
			lastEndedWithNewline = strings.HasSuffix(data, "\n")
			lineText := strings.TrimSuffix(data, "\n")
			lineText = strings.TrimSuffix(lineText, "\r")
			lineNumber++
			line := numberedLine{Number: lineNumber, Text: lineText}
			if tailCount > 0 {
				tail = append(tail, line)
				if len(tail) > tailCount {
					tail = tail[len(tail)-tailCount:]
				}
			} else if lineNumber >= requestedStart && lineNumber <= requestedEnd {
				selected = append(selected, line)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return lineWindow{}, readErr
		}
	}
	if info.Size() == 0 {
		lineNumber = 0
	} else if lastEndedWithNewline {
		lineNumber++
		empty := numberedLine{Number: lineNumber, Text: ""}
		if tailCount > 0 {
			tail = append(tail, empty)
			if len(tail) > tailCount {
				tail = tail[len(tail)-tailCount:]
			}
		} else if lineNumber >= requestedStart && lineNumber <= requestedEnd {
			selected = append(selected, empty)
		}
	}
	if tailCount > 0 {
		selected = tail
	}

	sha := hex.EncodeToString(hasher.Sum(nil))
	startLine := 0
	endLine := 0
	content := ""
	if len(selected) > 0 {
		startLine = selected[0].Number
		var builder strings.Builder
		width := len(strconv.Itoa(maxInt(lineNumber, 1)))
		byteLimit := contextOutputByteLimit()
		for index, line := range selected {
			rendered := fmt.Sprintf("%*d  %s", width, line.Number, compactPreviewLine(line.Text, maxPreviewLineBytes))
			separatorBytes := 0
			if builder.Len() > 0 {
				separatorBytes = 1
			}
			if builder.Len()+separatorBytes+len(rendered) > byteLimit {
				if builder.Len() > 0 {
					builder.WriteString("\n")
				}
				builder.WriteString("... window output byte budget reached; request a smaller range for more context ...")
				break
			}
			if index > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(rendered)
			endLine = line.Number
		}
		content = builder.String()
	}
	nextCursor := ""
	if endLine > 0 && endLine < lineNumber {
		nextCursor = makeLineCursor(endLine+1, sha)
	}
	previousCursor := ""
	if startLine > 1 {
		previousCursor = makeLineCursor(maxInt(1, startLine-defaultReadWindowLines), sha)
	}
	return lineWindow{
		Content:        content,
		StartLine:      startLine,
		EndLine:        endLine,
		TotalLines:     lineNumber,
		NextCursor:     nextCursor,
		PreviousCursor: previousCursor,
		Provenance: sourceProvenance{
			SHA256:       sha,
			Bytes:        info.Size(),
			Lines:        lineNumber,
			ModifiedUnix: info.ModTime().UnixNano(),
		},
	}, nil
}

func provenanceForFile(path string) (sourceProvenance, error) {
	file, err := os.Open(path)
	if err != nil {
		return sourceProvenance{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return sourceProvenance{}, err
	}
	if info.IsDir() {
		return sourceProvenance{}, fmt.Errorf("not a file: %s", path)
	}
	hasher := sha256.New()
	reader := bufio.NewReaderSize(io.TeeReader(file, hasher), 64*1024)
	lines := 0
	lastEndedWithNewline := false
	for {
		data, readErr := reader.ReadBytes('\n')
		if len(data) > 0 {
			if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
				return sourceProvenance{}, fmt.Errorf("file is binary or not valid UTF-8: %s", path)
			}
			lines++
			lastEndedWithNewline = data[len(data)-1] == '\n'
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return sourceProvenance{}, readErr
		}
	}
	if info.Size() == 0 {
		lines = 0
	} else if lastEndedWithNewline {
		lines++
	}
	return sourceProvenance{
		SHA256:       hex.EncodeToString(hasher.Sum(nil)),
		Bytes:        info.Size(),
		Lines:        lines,
		ModifiedUnix: info.ModTime().UnixNano(),
	}, nil
}

func provenanceForBytes(data []byte, info fs.FileInfo) sourceProvenance {
	hash := sha256.Sum256(data)
	return sourceProvenance{
		SHA256:       hex.EncodeToString(hash[:]),
		Bytes:        int64(len(data)),
		Lines:        countLines(string(data)),
		ModifiedUnix: info.ModTime().UnixNano(),
	}
}

func makeLineCursor(line int, sha string) string {
	if line < 1 {
		line = 1
	}
	if len(sha) > 12 {
		sha = sha[:12]
	}
	if sha == "" {
		return fmt.Sprintf("line:%d", line)
	}
	return fmt.Sprintf("line:%d:%s", line, sha)
}

func parseLineCursor(cursor string) (int, string, error) {
	parts := strings.Split(strings.TrimSpace(cursor), ":")
	if len(parts) < 2 || parts[0] != "line" {
		return 0, "", errors.New("invalid cursor; expected line:<number>[:sha256-prefix]")
	}
	line, err := strconv.Atoi(parts[1])
	if err != nil || line < 1 {
		return 0, "", errors.New("invalid cursor line number")
	}
	sha := ""
	if len(parts) >= 3 {
		sha = strings.ToLower(strings.TrimSpace(parts[2]))
		if len(sha) < 8 || len(sha) > 64 {
			return 0, "", errors.New("invalid cursor sha256 prefix")
		}
		if _, err := hex.DecodeString(sha); err != nil {
			return 0, "", errors.New("invalid cursor sha256 prefix")
		}
	}
	return line, sha, nil
}

func hashMatchesExpected(current, expected string) bool {
	current = strings.ToLower(strings.TrimSpace(current))
	expected = strings.ToLower(strings.TrimSpace(expected))
	return expected != "" && len(expected) <= len(current) && strings.HasPrefix(current, expected)
}

func withLineNumbersFrom(content string, firstLine int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	lastLine := firstLine + len(lines) - 1
	width := len(strconv.Itoa(maxInt(lastLine, 1)))
	var builder strings.Builder
	for index, line := range lines {
		fmt.Fprintf(&builder, "%*d  %s", width, firstLine+index, line)
		if index < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func boundedNumberedExcerpt(lines []string, start, end int) string {
	if len(lines) == 0 || start < 0 || start >= len(lines) || end < start {
		return ""
	}
	end = minInt(end, len(lines)-1)
	width := len(strconv.Itoa(maxInt(end+1, 1)))
	var builder strings.Builder
	for index := start; index <= end; index++ {
		fmt.Fprintf(&builder, "%*d  %s", width, index+1, compactPreviewLine(lines[index], maxPreviewLineBytes))
		if index < end {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func runShellCaptured(ctx context.Context, workspaceID, cwd, command string) (commandCaptureResult, int, error) {
	root, err := ensureArtifactStore()
	if err != nil {
		return runShellLegacyFallback(ctx, cwd, command)
	}
	raw, err := os.CreateTemp(filepath.Join(root, "tmp"), ".command-raw-*")
	if err != nil {
		return runShellLegacyFallback(ctx, cwd, command)
	}
	rawPath := raw.Name()
	defer os.Remove(rawPath)
	captureLimit := clampInt(envInt("DEVSPACE_MAX_ARTIFACT_BYTES", defaultArtifactBytes), maxCommandBytes, maxArtifactBytes)
	capture := &boundedCaptureWriter{file: raw, limit: int64(captureLimit)}

	cmd := shellCommand(ctx, command)
	cmd.Dir = cwd
	cmd.Stdout = capture
	cmd.Stderr = capture
	if err := cmd.Start(); err != nil {
		_ = raw.Close()
		return commandCaptureResult{}, -1, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		killProcessTree(cmd.Process.Pid)
		waitErr = <-done
		message := "\n"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			message += "[timeout] command exceeded the configured timeout\n"
		} else {
			message += "[canceled] request was canceled\n"
		}
		_, _ = capture.Write([]byte(message))
	}
	if err := raw.Sync(); err != nil && waitErr == nil {
		waitErr = err
	}
	if err := raw.Close(); err != nil && waitErr == nil {
		waitErr = err
	}

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	_, total, captureTruncated := capture.snapshot()
	result, finalizeErr := finalizeCommandCapture(root, rawPath, workspaceID, command, total, captureTruncated)
	if finalizeErr != nil {
		fallbackData, readErr := os.ReadFile(rawPath)
		if readErr != nil {
			return commandCaptureResult{}, exitCode, finalizeErr
		}
		redacted, changed := redactSensitiveText(string(fallbackData))
		trimmed, truncated := truncateUTF8(redacted, maxCommandBytes)
		return commandCaptureResult{
			Output:             trimmed,
			OriginalBytes:      total,
			StoredBytes:        int64(len(redacted)),
			TotalLines:         countLines(redacted),
			CaptureTruncated:   captureTruncated || truncated,
			Redacted:           changed,
			StorageUnavailable: true,
		}, exitCode, waitErr
	}
	return result, exitCode, waitErr
}

func runShellLegacyFallback(ctx context.Context, cwd, command string) (commandCaptureResult, int, error) {
	output, exitCode, truncated, runErr := runShell(ctx, cwd, command)
	redacted, changed := redactSensitiveText(output)
	return commandCaptureResult{
		Output:             redacted,
		OriginalBytes:      int64(len(output)),
		StoredBytes:        int64(len(redacted)),
		TotalLines:         countLines(redacted),
		CaptureTruncated:   truncated,
		Redacted:           changed,
		StorageUnavailable: true,
	}, exitCode, runErr
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if configured := strings.TrimSpace(os.Getenv("DEVSPACE_SHELL")); configured != "" {
		return exec.CommandContext(ctx, configured, "-c", command)
	}
	if runtimeShellIsWindows() {
		gitBash := `C:\Program Files\Git\bin\bash.exe`
		if _, err := os.Stat(gitBash); err == nil {
			return exec.CommandContext(ctx, gitBash, "-c", command)
		}
		return exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func runtimeShellIsWindows() bool {
	return filepath.Separator == '\\'
}

func finalizeCommandCapture(root, rawPath, workspaceID, command string, originalBytes int64, captureTruncated bool) (commandCaptureResult, error) {
	artifactStoreMutex.Lock()
	defer artifactStoreMutex.Unlock()

	raw, err := os.Open(rawPath)
	if err != nil {
		return commandCaptureResult{}, err
	}
	defer raw.Close()
	final, err := os.CreateTemp(filepath.Join(root, "tmp"), ".command-redacted-*")
	if err != nil {
		return commandCaptureResult{}, err
	}
	finalPath := final.Name()
	defer os.Remove(finalPath)
	hasher := sha256.New()
	writer := io.MultiWriter(final, hasher)
	reader := bufio.NewReaderSize(raw, 64*1024)
	collector := newPreviewCollector()
	inlineLimit := int64(clampInt(envInt("DEVSPACE_INLINE_COMMAND_BYTES", defaultInlineCommandBytes), minInlineCommandBytes, maxCommandBytes))
	var inline bytes.Buffer
	lineNumber := 0
	redactedAny := false
	storedRedactedBytes := int64(0)
	lastEndedWithNewline := false
	for {
		data, readErr := reader.ReadString('\n')
		if len(data) > 0 {
			lastEndedWithNewline = strings.HasSuffix(data, "\n")
			redacted, changed := redactSensitiveText(data)
			redactedAny = redactedAny || changed
			written, writeErr := io.WriteString(writer, redacted)
			if writeErr != nil {
				_ = final.Close()
				return commandCaptureResult{}, writeErr
			}
			storedRedactedBytes += int64(written)
			appendStringBounded(&inline, redacted, inlineLimit)
			lineText := strings.TrimSuffix(redacted, "\n")
			lineText = strings.TrimSuffix(lineText, "\r")
			lineNumber++
			collector.add(numberedLine{Number: lineNumber, Text: lineText})
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			_ = final.Close()
			return commandCaptureResult{}, readErr
		}
	}
	if storedRedactedBytes == 0 {
		lineNumber = 0
	} else if lastEndedWithNewline {
		lineNumber++
		collector.add(numberedLine{Number: lineNumber, Text: ""})
	}
	if err := final.Sync(); err != nil {
		_ = final.Close()
		return commandCaptureResult{}, err
	}
	if err := final.Close(); err != nil {
		return commandCaptureResult{}, err
	}

	if storedRedactedBytes <= inlineLimit && !captureTruncated {
		return commandCaptureResult{
			Output:           strings.TrimRight(inline.String(), "\r\n"),
			OriginalBytes:    originalBytes,
			StoredBytes:      storedRedactedBytes,
			TotalLines:       lineNumber,
			CaptureTruncated: false,
			Redacted:         redactedAny,
		}, nil
	}

	blobSHA := hex.EncodeToString(hasher.Sum(nil))
	blobPath := filepath.Join(root, "blobs", blobSHA+".txt")
	if _, err := os.Stat(blobPath); os.IsNotExist(err) {
		if err := os.Rename(finalPath, blobPath); err != nil {
			return commandCaptureResult{}, err
		}
	} else if err != nil {
		return commandCaptureResult{}, err
	}
	artifactID, err := newArtifactID()
	if err != nil {
		return commandCaptureResult{}, err
	}
	safeCommand, commandRedacted := redactSensitiveText(command)
	redactedAny = redactedAny || commandRedacted
	manifest := artifactManifest{
		Version:          1,
		ID:               artifactID,
		WorkspaceID:      workspaceID,
		BlobSHA256:       blobSHA,
		MIMEType:         "text/plain",
		SourceKind:       "command",
		SourceLabel:      safeCommand,
		CreatedAt:        time.Now().UTC(),
		StoredBytes:      storedRedactedBytes,
		OriginalBytes:    originalBytes,
		Lines:            lineNumber,
		CaptureTruncated: captureTruncated,
		Redacted:         redactedAny,
	}
	if err := writeArtifactManifest(root, manifest); err != nil {
		return commandCaptureResult{}, err
	}
	preview, ranges, omitted := collector.finish(lineNumber, contextOutputByteLimit())
	reference := artifactReference{
		ID:               artifactID,
		SHA256:           blobSHA,
		MIMEType:         manifest.MIMEType,
		StoredBytes:      storedRedactedBytes,
		OriginalBytes:    originalBytes,
		Lines:            lineNumber,
		IncludedRanges:   ranges,
		OmittedLines:     omitted,
		CaptureTruncated: captureTruncated,
		Redacted:         redactedAny,
	}
	_ = cleanupArtifactStoreLocked(root, artifactID)
	return commandCaptureResult{
		Output:           preview,
		Artifact:         &reference,
		OriginalBytes:    originalBytes,
		StoredBytes:      storedRedactedBytes,
		TotalLines:       lineNumber,
		IncludedRanges:   ranges,
		OmittedLines:     omitted,
		CaptureTruncated: captureTruncated,
		Redacted:         redactedAny,
	}, nil
}

func handleArtifactRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspaceID := argStr(req, "workspace_id")
	if _, err := getWorkspace(workspaceID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	artifactID := strings.ToLower(strings.TrimSpace(argStr(req, "artifact_id")))
	root, err := ensureArtifactStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	manifest, err := readArtifactManifest(root, artifactID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if manifest.WorkspaceID != workspaceID {
		return mcp.NewToolResultError("artifact does not belong to this workspace"), nil
	}
	blobPath, err := artifactBlobPath(root, manifest.BlobSHA256)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := strings.TrimSpace(argStr(req, "query"))
	reference := artifactReference{
		ID:               manifest.ID,
		SHA256:           manifest.BlobSHA256,
		MIMEType:         manifest.MIMEType,
		StoredBytes:      manifest.StoredBytes,
		OriginalBytes:    manifest.OriginalBytes,
		Lines:            manifest.Lines,
		CaptureTruncated: manifest.CaptureTruncated,
		Redacted:         manifest.Redacted,
	}
	view := artifactView{
		Kind:        "artifact",
		Title:       "Artifact",
		WorkspaceID: workspaceID,
		Artifact:    reference,
		SourceKind:  manifest.SourceKind,
		SourceLabel: manifest.SourceLabel,
		TotalLines:  manifest.Lines,
		Stats:       toolStats{Bytes: manifest.StoredBytes, Lines: manifest.Lines},
	}
	if query != "" {
		useRegex := argBool(req, "regex", false)
		caseSensitive := argBool(req, "case_sensitive", false)
		contextLines := clampInt(argInt(req, "context_lines", defaultSearchContextLines), 0, maxSearchContextLines)
		maxResults := clampInt(argInt(req, "max_results", defaultSearchResults), 1, maxSearchResults)
		matches, truncated, searchErr := searchOneFile(blobPath, artifactID, query, useRegex, caseSensitive, contextLines, maxResults)
		if searchErr != nil {
			return mcp.NewToolResultError(searchErr.Error()), nil
		}
		view.Matches = matches
		view.Summary = fmt.Sprintf("%d matches in %s", len(matches), artifactID)
		view.Truncated = truncated
		fallback := fmt.Sprintf("Found %d matches in artifact %s.", len(matches), artifactID)
		return newToolResult("artifact_read", view, view, fallback), nil
	}

	startLine := argInt(req, "start_line", 0)
	endLine := argInt(req, "end_line", 0)
	tail := argInt(req, "tail", 0)
	maxLines := clampInt(argInt(req, "max_lines", defaultReadWindowLines), 1, maxReadWindowLines)
	cursor := strings.TrimSpace(argStr(req, "cursor"))
	if cursor != "" {
		cursorLine, _, cursorErr := parseLineCursor(cursor)
		if cursorErr != nil {
			return mcp.NewToolResultError(cursorErr.Error()), nil
		}
		startLine = cursorLine
	}
	if tail == 0 {
		if startLine <= 0 {
			startLine = 1
		}
		if endLine <= 0 {
			endLine = startLine + maxLines - 1
		}
	}
	window, err := readLineWindow(blobPath, startLine, endLine, clampInt(tail, 0, maxReadWindowLines))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	view.Content = window.Content
	view.StartLine = window.StartLine
	view.EndLine = window.EndLine
	view.TotalLines = window.TotalLines
	view.NextCursor = window.NextCursor
	view.PreviousCursor = window.PreviousCursor
	view.Truncated = window.StartLine > 1 || window.EndLine < window.TotalLines || manifest.CaptureTruncated
	view.Summary = fmt.Sprintf("lines %d-%d of %d", window.StartLine, window.EndLine, window.TotalLines)
	fallback := fmt.Sprintf("Read artifact %s lines %d-%d of %d.", artifactID, window.StartLine, window.EndLine, window.TotalLines)
	return newToolResult("artifact_read", view, view, fallback), nil
}

func searchOneFile(path, displayPath, query string, useRegex, caseSensitive bool, contextLines, maxResults int) ([]searchMatch, bool, error) {
	matcher, err := compileSearchMatcher(query, useRegex, caseSensitive)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, false, errors.New("artifact is not valid UTF-8 text")
	}
	lines := strings.Split(string(data), "\n")
	matches := make([]searchMatch, 0, minInt(maxResults, 16))
	maxOutputBytes := contextOutputByteLimit()
	outputBytes := 0
	for lineIndex, line := range lines {
		column := matcher(line)
		if column < 0 {
			continue
		}
		start := maxInt(0, lineIndex-contextLines)
		end := minInt(len(lines)-1, lineIndex+contextLines)
		excerpt := boundedNumberedExcerpt(lines, start, end)
		if outputBytes+len(excerpt) > maxOutputBytes {
			return matches, true, nil
		}
		outputBytes += len(excerpt)
		matches = append(matches, searchMatch{
			Path:       displayPath,
			Line:       lineIndex + 1,
			Column:     column + 1,
			Excerpt:    excerpt,
			TotalLines: len(lines),
		})
		if len(matches) >= maxResults {
			return matches, true, nil
		}
	}
	return matches, false, nil
}

func ensureArtifactStore() (string, error) {
	root := strings.TrimSpace(os.Getenv("DEVSPACE_ARTIFACT_DIR"))
	if root == "" {
		runtimeDir := strings.TrimSpace(os.Getenv("DEVSPACE_RUNTIME_DIR"))
		if runtimeDir == "" {
			runtimeDir = "runtime"
		}
		root = filepath.Join(runtimeDir, "artifacts")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	for _, directory := range []string{absolute, filepath.Join(absolute, "blobs"), filepath.Join(absolute, "manifests"), filepath.Join(absolute, "tmp")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return "", err
		}
	}
	cleanupStaleArtifactTemps(filepath.Join(absolute, "tmp"), 30*time.Minute)
	return absolute, nil
}

func cleanupStaleArtifactTemps(directory string, minimumAge time.Duration) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-minimumAge)
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasPrefix(entry.Name(), ".command-raw-") && !strings.HasPrefix(entry.Name(), ".command-redacted-")) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(directory, entry.Name()))
		}
	}
}

func newArtifactID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return "art_" + hex.EncodeToString(data), nil
}

func writeArtifactManifest(root string, manifest artifactManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(root, "manifests", manifest.ID+".json"), append(data, '\n'), 0o600)
}

func readArtifactManifest(root, artifactID string) (artifactManifest, error) {
	if !artifactIDRE.MatchString(artifactID) {
		return artifactManifest{}, errors.New("invalid artifact_id")
	}
	path := filepath.Join(root, "manifests", artifactID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return artifactManifest{}, errors.New("artifact not found")
		}
		return artifactManifest{}, err
	}
	var manifest artifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return artifactManifest{}, err
	}
	if manifest.ID != artifactID || manifest.Version != 1 {
		return artifactManifest{}, errors.New("invalid artifact manifest")
	}
	return manifest, nil
}

func artifactBlobPath(root, sha string) (string, error) {
	if len(sha) != 64 {
		return "", errors.New("invalid artifact sha256")
	}
	if _, err := hex.DecodeString(sha); err != nil {
		return "", errors.New("invalid artifact sha256")
	}
	path := filepath.Join(root, "blobs", sha+".txt")
	cleanRoot := filepath.Clean(filepath.Join(root, "blobs"))
	cleanPath := filepath.Clean(path)
	if !pathInsideRoot(cleanRoot, cleanPath) {
		return "", errors.New("artifact path escaped the artifact store")
	}
	return cleanPath, nil
}

func cleanupArtifactStoreLocked(root, protectedArtifactID string) error {
	quota := int64(envInt("DEVSPACE_ARTIFACT_QUOTA_BYTES", defaultArtifactQuotaBytes))
	if quota < minArtifactQuotaBytes {
		quota = minArtifactQuotaBytes
	}
	blobEntries, err := os.ReadDir(filepath.Join(root, "blobs"))
	if err != nil {
		return err
	}
	total := int64(0)
	for _, entry := range blobEntries {
		if entry.IsDir() {
			continue
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
	}
	if total <= quota {
		return nil
	}

	type manifestEntry struct {
		Manifest artifactManifest
		Path     string
	}
	var manifests []manifestEntry
	manifestFiles, err := os.ReadDir(filepath.Join(root, "manifests"))
	if err != nil {
		return err
	}
	for _, entry := range manifestFiles {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(root, "manifests", entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var manifest artifactManifest
		if json.Unmarshal(data, &manifest) == nil {
			manifests = append(manifests, manifestEntry{Manifest: manifest, Path: path})
		}
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Manifest.CreatedAt.Before(manifests[j].Manifest.CreatedAt) })
	for _, entry := range manifests {
		if total <= quota {
			break
		}
		if entry.Manifest.ID == protectedArtifactID {
			continue
		}
		_ = os.Remove(entry.Path)
		referenced := false
		for _, other := range manifests {
			if other.Manifest.ID == entry.Manifest.ID {
				continue
			}
			if _, statErr := os.Stat(other.Path); statErr == nil && other.Manifest.BlobSHA256 == entry.Manifest.BlobSHA256 {
				referenced = true
				break
			}
		}
		if !referenced {
			blobPath, pathErr := artifactBlobPath(root, entry.Manifest.BlobSHA256)
			if pathErr == nil {
				if info, statErr := os.Stat(blobPath); statErr == nil {
					total -= info.Size()
				}
				_ = os.Remove(blobPath)
			}
		}
	}
	return nil
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolInt(condition bool, value int) int {
	if condition {
		return value
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
