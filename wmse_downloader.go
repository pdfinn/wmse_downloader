// wmse_downloader.go
//
// A gentle downloader for WMSE MP3 archives. It first gets the show ID from the program page,
// then fetches archive links from the API, and finally downloads each MP3 file with a pause
// between requests to avoid overloading the server.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Constants for validation and limits
const (
	maxShowIDLength  = 50                // Maximum length for show ID
	maxResponseSize  = 10 * 1024 * 1024  // 10MB max response size
	maxFileSize      = 500 * 1024 * 1024 // 500MB max file size
	maxArchiveLinks  = 1000              // Maximum number of archive links to process
	validShowIDRegex = `^[a-zA-Z0-9_-]+$`
	baseURL          = "https://wmse.org"
	apiURL           = "https://wmse.fly.dev"
)

var (
	ErrInvalidShowID      = errors.New("invalid show ID")
	ErrResponseTooLarge   = errors.New("response too large")
	ErrFileTooLarge       = errors.New("file too large")
	ErrInvalidContentType = errors.New("invalid content type")
	ErrTooManyLinks       = errors.New("too many archive links")
)

// Show represents a WMSE show episode
type Show struct {
	ID         string    `json:"show_id"`
	Name       string    `json:"show_name"`
	ArchiveURL string    `json:"archive_url"`
	Date       time.Time `json:"playlist_date"`
}

// Archive represents a WMSE show archive
type Archive struct {
	ShowID       string  `json:"show_id"`
	ArchiveURL   string  `json:"archive_url"`
	PlaylistID   *string `json:"playlist_id"`
	PlaylistDate string  `json:"playlist_date"`
}

// validateShowID ensures the show ID meets security requirements
func validateShowID(id string) error {
	if id == "" || len(id) > maxShowIDLength {
		return fmt.Errorf("%w: empty or too long", ErrInvalidShowID)
	}

	matched, err := regexp.MatchString(validShowIDRegex, id)
	if err != nil || !matched {
		return fmt.Errorf("%w: contains invalid characters", ErrInvalidShowID)
	}

	return nil
}

// sanitizeFilename ensures the filename is safe for filesystem operations
func sanitizeFilename(filename string) string {
	// Remove any directory traversal attempts
	filename = filepath.Base(filename)

	// Remove any non-alphanumeric characters except for dots and hyphens
	reg := regexp.MustCompile(`[^a-zA-Z0-9.-]`)
	filename = reg.ReplaceAllString(filename, "_")

	// Ensure it ends with .mp3
	if !strings.HasSuffix(strings.ToLower(filename), ".mp3") {
		filename += ".mp3"
	}

	return filename
}

// getShowArchiveID gets the archive ID from the program page
func getShowArchiveID(ctx context.Context, showID string) (string, error) {
	logger := slog.Default()

	// Validate show ID
	if err := validateShowID(showID); err != nil {
		return "", err
	}

	// Create request with context
	url := fmt.Sprintf("%s/program/%s/", baseURL, showID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers to look like a browser
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.4 Safari/605.1.15")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Perform request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch program page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("program page returned non-200 status: %s", resp.Status)
	}

	// Parse HTML
	doc, err := html.Parse(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Find the wmse-archive element and get its show-id attribute
	var archiveID string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "wmse-archive" {
			for _, attr := range n.Attr {
				if attr.Key == "show-id" {
					archiveID = attr.Val
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	if archiveID == "" {
		return "", fmt.Errorf("could not find archive ID on page")
	}

	logger.Info("Found archive ID", "id", archiveID)
	return archiveID, nil
}

// fetchArchives gets the list of archives from the API
func fetchArchives(ctx context.Context, archiveID string) ([]Archive, error) {
	logger := slog.Default()

	// Create request with context
	url := fmt.Sprintf("%s/api/shows/%s", apiURL, archiveID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.4 Safari/605.1.15")

	// Perform request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch archives: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned non-200 status: %s", resp.Status)
	}

	// Parse JSON response
	var archives []Archive
	if err := json.NewDecoder(resp.Body).Decode(&archives); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	logger.Info("Found archives",
		"count", len(archives),
		"archive_id", archiveID)

	return archives, nil
}

// downloadShow downloads a single show's MP3 file
func downloadShow(archive Archive, outputDir string, delay time.Duration) error {
	logger := slog.Default()

	if archive.ArchiveURL == "" {
		return fmt.Errorf("no MP3 URL available for archive: %s", archive.ShowID)
	}

	// Create a filename from the show date and ID
	filename := fmt.Sprintf("%s_%s.mp3", archive.PlaylistDate, archive.ShowID)
	filename = sanitizeFilename(filename)
	outputPath := filepath.Join(outputDir, filename)

	// Skip if already downloaded
	if _, err := os.Stat(outputPath); err == nil {
		logger.Info("Skipping existing file", "filename", filename)
		return nil
	}

	logger.Info("Downloading show",
		"date", archive.PlaylistDate,
		"url", archive.ArchiveURL)

	// Create output directory if needed
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("could not create output directory: %w", err)
	}

	// Stream to temporary file first
	tempFile := outputPath + ".tmp"
	outFile, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("could not create temp file %s: %w", tempFile, err)
	}
	defer func() {
		outFile.Close()
		if err != nil {
			os.Remove(tempFile)
		}
	}()

	// Retry logic for downloads
	maxRetries := 3
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			logger.Info("Retrying download",
				"attempt", attempt,
				"max_retries", maxRetries,
				"previous_error", lastErr)
			time.Sleep(time.Second * time.Duration(attempt*2)) // Exponential backoff
		}

		// Create request with longer timeout
		req, err := http.NewRequest("GET", archive.ArchiveURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.4 Safari/605.1.15")

		// Use a longer timeout for downloads
		client := &http.Client{
			Timeout: 30 * time.Minute, // Increased from 10 minutes
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to GET %s: %w", archive.ArchiveURL, err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("bad status downloading %s: %s", archive.ArchiveURL, resp.Status)
			continue
		}

		// Create a progress reader
		progressReader := &progressReader{
			reader: resp.Body,
			total:  resp.ContentLength,
			onProgress: func(written int64) {
				if written%1024 == 0 { // Log every 1KB
					logger.Debug("Download progress",
						"filename", filename,
						"written", written,
						"total", resp.ContentLength)
				}
			},
		}

		// Copy with size limit
		written, err := io.Copy(outFile, io.LimitReader(progressReader, maxFileSize+1))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("error writing to %s: %w", tempFile, err)
			continue
		}
		if written > maxFileSize {
			lastErr = ErrFileTooLarge
			continue
		}

		// Success - break retry loop
		lastErr = nil
		break
	}

	if lastErr != nil {
		return lastErr
	}

	// Sync to ensure all data is written
	if err := outFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Close the file before renaming
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	// Atomic rename from temp to final
	if err := os.Rename(tempFile, outputPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	logger.Info("Downloaded file",
		"filename", filename)

	time.Sleep(delay)
	return nil
}

// progressReader wraps an io.Reader to track progress
type progressReader struct {
	reader     io.Reader
	total      int64
	written    int64
	onProgress func(written int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.written += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(pr.written)
		}
	}
	return n, err
}

func main() {
	// Setup logging
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	// Command‑line flags
	showID := flag.String("show", "ded", "ID of the WMSE show to download archives for")
	outDir := flag.String("out", "./archives", "Directory to save MP3 files")
	delay := flag.Duration("delay", 5*time.Second, "Delay between downloads to avoid hammering")
	flag.Parse()

	logger.Info("Starting archive download",
		"show_id", *showID,
		"output_dir", *outDir)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// First get the archive ID from the program page
	archiveID, err := getShowArchiveID(ctx, *showID)
	if err != nil {
		logger.Error("Failed to get archive ID", "error", err)
		os.Exit(1)
	}

	// Then fetch archives from the API
	archives, err := fetchArchives(ctx, archiveID)
	if err != nil {
		logger.Error("Failed to fetch archives", "error", err)
		os.Exit(1)
	}

	if len(archives) == 0 {
		logger.Error("No archives found", "show_id", *showID)
		os.Exit(1)
	}

	// Download each show
	for _, archive := range archives {
		if err := downloadShow(archive, *outDir, *delay); err != nil {
			logger.Error("Download failed",
				"archive", archive.ShowID,
				"error", err)
		}
	}
}
