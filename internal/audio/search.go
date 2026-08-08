package audio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SearchResult holds the metadata found by yt-dlp search.
type SearchResult struct {
	Title     string
	Query     string // original query or resolved URL for downloading
	Duration  string
	Thumbnail string
	Uploader  string
}

// Search finds a track using yt-dlp but does NOT download it.
// It returns the title, URL, and metadata (which yt-dlp can use to extract media later).
func Search(ctx context.Context, query string) (*SearchResult, error) {
	isURL := strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://")

	if isURL {
		if strings.Contains(query, "spotify.com") {
			log.Printf("[AFK-BOT] [AUDIO] Detected Spotify URL, extracting metadata via yt-dlp...")
			// yt-dlp can extract metadata from Spotify (but cannot download audio).
			title, _, _, _, uploader, err := getTrackInfo(ctx, query)
			if err == nil && title != "" {
				scQuery := title
				if uploader != "" && uploader != "NA" {
					scQuery = uploader + " " + title
				}
				log.Printf("[AFK-BOT] [AUDIO] Converted Spotify URL to SoundCloud search: %s", scQuery)
				// Recursively search SoundCloud using the extracted metadata
				return Search(ctx, scQuery)
			}
			log.Printf("[AFK-BOT] [AUDIO] Failed to extract Spotify metadata: %v. Proceeding with raw URL...", err)
		}

		// For direct URLs, just get the title
		title, _, duration, thumb, uploader, err := getTrackInfo(ctx, query)
		if err != nil {
			return nil, err
		}
		return &SearchResult{
			Title: title, Query: query, Duration: duration, Thumbnail: thumb, Uploader: uploader,
		}, nil
	}

	// Attempt SoundCloud search first
	log.Printf("[AFK-BOT] [AUDIO] Searching SoundCloud: %s", query)
	title, webpageURL, duration, thumb, uploader, err := getTrackInfo(ctx, fmt.Sprintf("scsearch:%s", query))
	
	// If SoundCloud returns a 30-second preview (common for major labels), or fails, fallback to YouTube
	if err != nil || duration == "0:30" || duration == "00:30" || duration == "30" {
		log.Printf("[AFK-BOT] [AUDIO] SoundCloud returned 30s preview or error (%v). Falling back to YouTube...", err)
		
		ytTitle, ytURL, ytDur, ytThumb, ytUploader, ytErr := getTrackInfo(ctx, fmt.Sprintf("ytsearch:%s", query))
		if ytErr == nil {
			return &SearchResult{
				Title: ytTitle, Query: ytURL, Duration: ytDur, Thumbnail: ytThumb, Uploader: ytUploader,
			}, nil
		}
		
		log.Printf("[AFK-BOT] [AUDIO] YouTube fallback failed: %v", ytErr)
		
		// If both fail, and we had a valid 30s SoundCloud preview, just return it as a last resort
		if err == nil {
			log.Printf("[AFK-BOT] [AUDIO] Using 30s preview as last resort")
			return &SearchResult{
				Title: title + " (30s Preview)", Query: webpageURL, Duration: duration, Thumbnail: thumb, Uploader: uploader,
			}, nil
		}
		
		return nil, fmt.Errorf("failed to find track on both SoundCloud and YouTube: %w", ytErr)
	}

	return &SearchResult{
		Title: title, Query: webpageURL, Duration: duration, Thumbnail: thumb, Uploader: uploader,
	}, nil
}

// getTrackInfo extracts the metadata from yt-dlp without downloading.
func getTrackInfo(ctx context.Context, query string) (title, webpageURL, duration, thumbnail, uploader string, err error) {
	// Add a 45-second timeout for the search so it doesn't hang forever, but gives yt-dlp enough time to parse cookies
	searchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	args := []string{
		"--force-ipv4",
		"--no-download",
		"--retries", "5",
		"--extractor-args", "youtube:player_client=android,web",
		"--print", "%(title)s\n%(webpage_url)s\n%(duration_string)s\n%(thumbnail)s\n%(uploader)s",
		"--no-warnings",
		"--no-playlist",
	}

	if stat, err := os.Stat("www.youtube.com_cookies.txt"); err == nil && stat.Size() > 0 {
		args = append(args, "--cookies", "www.youtube.com_cookies.txt")
		log.Printf("[AFK-BOT] [AUDIO] Using www.youtube.com_cookies.txt for search...")
	} else if stat, err := os.Stat("cookies.txt"); err == nil && stat.Size() > 0 {
		args = append(args, "--cookies", "cookies.txt")
		log.Printf("[AFK-BOT] [AUDIO] Using cookies.txt for search...")
	}

	args = append(args, query)
	cmd := exec.CommandContext(searchCtx, "yt-dlp", args...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("[AFK-BOT] [AUDIO] yt-dlp search error: %s", string(exitErr.Stderr))
		}
		return "", "", "", "", "", err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		if len(lines) == 1 && lines[0] != "" {
			return lines[0], query, "00:00", "", "", nil
		}
		return "", "", "", "", "", fmt.Errorf("yt-dlp returned incomplete info")
	}

	title = strings.TrimSpace(lines[0])
	webpageURL = strings.TrimSpace(lines[1])

	if len(lines) >= 5 {
		duration = strings.TrimSpace(lines[2])
		thumbnail = strings.TrimSpace(lines[3])
		uploader = strings.TrimSpace(lines[4])
	}

	if title == "" || webpageURL == "" {
		return "", "", "", "", "", fmt.Errorf("yt-dlp returned empty title or URL")
	}
	return title, webpageURL, duration, thumbnail, uploader, nil
}

// Legacy wrappers for backwards compatibility
func SearchAndExtract(ctx context.Context, query string) (string, string, error) {
	result, err := Search(ctx, query)
	if err != nil {
		return "", "", err
	}
	return result.Title, result.Query, nil
}
