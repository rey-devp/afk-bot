package audio

import (
	"context"
	"errors"
	"fmt"
	"log"
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

	// 1. Attempt YouTube Video search first
	log.Printf("[AFK-BOT] [AUDIO] Searching YouTube Video: %s", query)
	title, webpageURL, duration, thumb, uploader, err := getTrackInfo(ctx, fmt.Sprintf("ytsearch1:%s", query))

	// If YouTube Video fails, fallback to SoundCloud
	if err != nil {
		log.Printf("[AFK-BOT] [AUDIO] YouTube Video failed (%v). Falling back to SoundCloud...", err)
		scTitle, scURL, scDur, scThumb, scUploader, scErr := getTrackInfo(ctx, fmt.Sprintf("scsearch:%s", query))
		
		if scErr == nil {
			// Check for 30s preview
			if scDur == "0:30" || scDur == "00:30" || scDur == "30" {
				log.Printf("[AFK-BOT] [AUDIO] SoundCloud returned 30s preview as last resort")
				return &SearchResult{
					Title: scTitle + " (30s Preview)", Query: scURL, Duration: scDur, Thumbnail: scThumb, Uploader: scUploader,
				}, nil
			}

			// Valid SoundCloud track
			return &SearchResult{
				Title: scTitle, Query: scURL, Duration: scDur, Thumbnail: scThumb, Uploader: scUploader,
			}, nil
		}

		// Both YouTube Video and SoundCloud failed
		return nil, fmt.Errorf("failed to find track on all platforms: %w", scErr)
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
		"--ignore-no-formats-error", // IMPORTANT: Skip format extraction errors for YouTube
		"--remote-components", "ejs:github", // Solve YouTube JS challenge
		"--js-runtimes", "node", // Tell yt-dlp to explicitly use node
		"--retries", "5",
		"--print", "%(title)s\n%(webpage_url)s\n%(duration_string)s\n%(thumbnail)s\n%(uploader)s",
		"--no-warnings",
		"--no-playlist",
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
