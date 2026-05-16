package pdf

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DocumentInfo holds metadata about a PDF file.
type DocumentInfo struct {
	Filename  string `json:"filename"`
	Title     string `json:"title"`
	Pages     int    `json:"pages"`
	SizeBytes int64  `json:"size_bytes"`
}

// cacheEntry stores extracted text with its modification time for invalidation.
type cacheEntry struct {
	text  string
	mtime time.Time
}

// Reader handles PDF text extraction via pdftotext with in-memory caching.
type Reader struct {
	docsDir      string
	pdftotextBin string
	pdfinfoBin   string

	mu    sync.RWMutex
	cache map[string]*cacheEntry
}

// NewReader creates a new PDF reader for the given documents directory.
func NewReader(docsDir string) *Reader {
	pdftotextBin := "pdftotext"
	pdfinfoBin := "pdfinfo"

	// Check for Homebrew installation on macOS
	if _, err := os.Stat("/opt/homebrew/bin/pdftotext"); err == nil {
		pdftotextBin = "/opt/homebrew/bin/pdftotext"
	}
	if _, err := os.Stat("/opt/homebrew/bin/pdfinfo"); err == nil {
		pdfinfoBin = "/opt/homebrew/bin/pdfinfo"
	}

	return &Reader{
		docsDir:      docsDir,
		pdftotextBin: pdftotextBin,
		pdfinfoBin:   pdfinfoBin,
		cache:        make(map[string]*cacheEntry),
	}
}

// CheckDependencies verifies that pdftotext and pdfinfo are available.
func (r *Reader) CheckDependencies() error {
	if _, err := exec.LookPath(r.pdftotextBin); err != nil {
		return fmt.Errorf("pdftotext not found at %s: install poppler (brew install poppler)", r.pdftotextBin)
	}
	if _, err := exec.LookPath(r.pdfinfoBin); err != nil {
		return fmt.Errorf("pdfinfo not found at %s: install poppler (brew install poppler)", r.pdfinfoBin)
	}
	return nil
}

// sanitizeFilename ensures the filename is safe (no directory traversal) and is a .pdf file.
func (r *Reader) sanitizeFilename(filename string) (string, error) {
	base := filepath.Base(filename)
	if base != filename {
		return "", fmt.Errorf("invalid filename: directory traversal not allowed")
	}
	if !strings.HasSuffix(strings.ToLower(base), ".pdf") {
		return "", fmt.Errorf("invalid filename: only .pdf files are supported")
	}
	return base, nil
}

// fullPath returns the full filesystem path for a sanitized filename.
func (r *Reader) fullPath(filename string) string {
	return filepath.Join(r.docsDir, filename)
}

// ListDocuments returns metadata for all PDF files in the documents directory.
func (r *Reader) ListDocuments() ([]DocumentInfo, error) {
	entries, err := os.ReadDir(r.docsDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read documents directory %s: %w", r.docsDir, err)
	}

	var docs []DocumentInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		pages := r.getPageCount(filepath.Join(r.docsDir, name))
		title := strings.TrimSuffix(name, filepath.Ext(name))

		docs = append(docs, DocumentInfo{
			Filename:  name,
			Title:     title,
			Pages:     pages,
			SizeBytes: info.Size(),
		})
	}

	return docs, nil
}

// getPageCount uses pdfinfo to get the page count for a PDF file.
func (r *Reader) getPageCount(path string) int {
	cmd := exec.Command(r.pdfinfoBin, path)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Pages:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				n, err := strconv.Atoi(parts[1])
				if err == nil {
					return n
				}
			}
		}
	}
	return 0
}

// ReadDocument extracts text from a PDF file. If page > 0, only that page is returned.
func (r *Reader) ReadDocument(filename string, page int) (string, error) {
	safe, err := r.sanitizeFilename(filename)
	if err != nil {
		return "", err
	}

	path := r.fullPath(safe)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("document not found: %s", safe)
	}

	if page > 0 {
		return r.extractPage(path, page, page)
	}

	return r.extractFull(safe, path)
}

// extractFull extracts the full text of a PDF, using cache when possible.
func (r *Reader) extractFull(filename, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot stat file: %w", err)
	}
	mtime := info.ModTime()

	// Check cache
	r.mu.RLock()
	if entry, ok := r.cache[filename]; ok && entry.mtime.Equal(mtime) {
		r.mu.RUnlock()
		return entry.text, nil
	}
	r.mu.RUnlock()

	// Extract text
	text, err := r.runPdftotext(path, 0, 0)
	if err != nil {
		return "", err
	}

	// Update cache
	r.mu.Lock()
	r.cache[filename] = &cacheEntry{text: text, mtime: mtime}
	r.mu.Unlock()

	return text, nil
}

// extractPage extracts text from a specific page range.
func (r *Reader) extractPage(path string, firstPage, lastPage int) (string, error) {
	return r.runPdftotext(path, firstPage, lastPage)
}

// runPdftotext invokes pdftotext with the given options.
func (r *Reader) runPdftotext(path string, firstPage, lastPage int) (string, error) {
	args := []string{"-layout"}
	if firstPage > 0 {
		args = append(args, "-f", strconv.Itoa(firstPage))
	}
	if lastPage > 0 {
		args = append(args, "-l", strconv.Itoa(lastPage))
	}
	args = append(args, path, "-")

	cmd := exec.Command(r.pdftotextBin, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext failed: %w", err)
	}
	return string(out), nil
}

// SearchDocument searches a document for lines matching the query (case-insensitive).
// Returns matching lines with 2 lines of context before and after, with page number hints.
func (r *Reader) SearchDocument(filename, query string) (string, error) {
	safe, err := r.sanitizeFilename(filename)
	if err != nil {
		return "", err
	}

	path := r.fullPath(safe)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("document not found: %s", safe)
	}

	text, err := r.extractFull(safe, path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(text, "\n")
	queryLower := strings.ToLower(query)
	contextLines := 2

	var results []string
	matchCount := 0
	pageNum := 1

	for i, line := range lines {
		// Track page breaks (form feed character)
		if strings.Contains(line, "\f") {
			pageNum++
		}

		if strings.Contains(strings.ToLower(line), queryLower) {
			matchCount++
			start := i - contextLines
			if start < 0 {
				start = 0
			}
			end := i + contextLines + 1
			if end > len(lines) {
				end = len(lines)
			}

			results = append(results, fmt.Sprintf("--- Match %d (page ~%d, line %d) ---", matchCount, pageNum, i+1))
			for j := start; j < end; j++ {
				prefix := "  "
				if j == i {
					prefix = "> "
				}
				results = append(results, prefix+lines[j])
			}
			results = append(results, "")
		}
	}

	if matchCount == 0 {
		return fmt.Sprintf("No matches found for '%s' in %s", query, safe), nil
	}

	header := fmt.Sprintf("Found %d matches for '%s' in %s:\n\n", matchCount, query, safe)
	return header + strings.Join(results, "\n"), nil
}

// GetDocumentSummary returns the first 3 pages of text as a summary.
func (r *Reader) GetDocumentSummary(filename string) (string, error) {
	safe, err := r.sanitizeFilename(filename)
	if err != nil {
		return "", err
	}

	path := r.fullPath(safe)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("document not found: %s", safe)
	}

	text, err := r.extractPage(path, 1, 3)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Summary (first 3 pages) of %s:\n\n%s", safe, text), nil
}
