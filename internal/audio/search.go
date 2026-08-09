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
func Search(ctx context.Context, guildID, query string) (*SearchResult, error) {
	isURL := strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://")

	if isURL {
		if strings.Contains(query, "spotify.com") {
			log.Printf("[AFK-BOT] [%s] [AUDIO] Detected Spotify URL, extracting metadata via yt-dlp...", guildID)
			// yt-dlp can extract metadata from Spotify (but cannot download audio).
			title, _, _, _, uploader, err := getTrackInfo(ctx, guildID, query)
			if err == nil && title != "" {
				scQuery := title
				if uploader != "" && uploader != "NA" {
					scQuery = uploader + " " + title
				}
				log.Printf("[AFK-BOT] [%s] [AUDIO] Converted Spotify URL to SoundCloud search: %s", guildID, scQuery)
				// Recursively search SoundCloud using the extracted metadata
				return Search(ctx, guildID, scQuery)
			}
			log.Printf("[AFK-BOT] [%s] [AUDIO] Failed to extract Spotify metadata: %v. Proceeding with raw URL...", guildID, err)
		}

		// For direct URLs, just get the title
		title, _, duration, thumb, uploader, err := getTrackInfo(ctx, guildID, query)
		if err != nil {
			return nil, err
		}
		return &SearchResult{
			Title: title, Query: query, Duration: duration, Thumbnail: thumb, Uploader: uploader,
		}, nil
	}

	isYouTubeQuery := !strings.HasPrefix(query, "scsearch:") // prevent double scsearch nesting

	var title, webpageURL, duration, thumb, uploader string
	var err error

	if isYouTubeQuery {
		if inCooldown, remaining := CheckYtCooldown(guildID); inCooldown {
			log.Printf("[AFK-BOT] [%s] [AUDIO] Guild still in bot-detection cooldown (%ds remaining), skipping YouTube search", guildID, int(remaining.Seconds()))
			// Force error to trigger SoundCloud fallback below
			err = fmt.Errorf("YOUTUBE_BLOCKED_COOLDOWN")
		} else {
			// 1. Attempt YouTube Video search first
			log.Printf("[AFK-BOT] [%s] [AUDIO] Searching YouTube Video: %s", guildID, query)
			title, webpageURL, duration, thumb, uploader, err = getTrackInfo(ctx, guildID, fmt.Sprintf("ytsearch1:%s", query))
		}
	} else {
		err = fmt.Errorf("forced soundcloud search")
	}

	// If YouTube Video fails (or was skipped due to cooldown), fallback to SoundCloud
	if err != nil {
		log.Printf("[AFK-BOT] [%s] [AUDIO] YouTube Video failed (%v). Falling back to SoundCloud...", guildID, err)
		scTitle, scURL, scDur, scThumb, scUploader, scErr := getTrackInfo(ctx, guildID, fmt.Sprintf("scsearch:%s", query))
		
		if scErr == nil {
			// Check for 30s preview
			if scDur == "0:30" || scDur == "00:30" || scDur == "30" {
				log.Printf("[AFK-BOT] [%s] [AUDIO] SoundCloud returned 30s preview as last resort", guildID)
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

// SearchMany finds multiple tracks (up to limit) and returns them as a slice of SearchResults.
func SearchMany(ctx context.Context, guildID, query string, limit int) ([]SearchResult, error) {
	isURL := strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://")

	// If it's a direct URL or Spotify URL, we fallback to just returning the single item
	if isURL {
		res, err := Search(ctx, guildID, query)
		if err != nil {
			return nil, err
		}
		return []SearchResult{*res}, nil
	}

	if inCooldown, remaining := CheckYtCooldown(guildID); inCooldown {
		log.Printf("[AFK-BOT] [%s] [AUDIO] Guild still in bot-detection cooldown (%ds remaining), skipping YouTube search", guildID, int(remaining.Seconds()))
		// Fallback to soundcloud directly
		scResults, scErr := getTrackInfoMany(ctx, guildID, fmt.Sprintf("scsearch%d:%s", limit, query))
		if scErr == nil && len(scResults) > 0 {
			return scResults, nil
		}
		return nil, fmt.Errorf("YOUTUBE_BLOCKED_COOLDOWN: Sign in to confirm you're not a bot")
	}

	log.Printf("[AFK-BOT] [%s] [AUDIO] Searching YouTube Video (Multiple): %s", guildID, query)
	results, err := getTrackInfoMany(ctx, guildID, fmt.Sprintf("ytsearch%d:%s", limit, query))
	
	if err != nil {
		log.Printf("[AFK-BOT] [%s] [AUDIO] YouTube Video multiple search failed (%v). Falling back to SoundCloud...", guildID, err)
		scResults, scErr := getTrackInfoMany(ctx, guildID, fmt.Sprintf("scsearch%d:%s", limit, query))
		if scErr == nil && len(scResults) > 0 {
			return scResults, nil
		}
		return nil, fmt.Errorf("failed to find tracks on all platforms: %w", scErr)
	}

	return results, nil
}

// getTrackInfo extracts the metadata from yt-dlp without downloading.
func getTrackInfo(ctx context.Context, guildID, query string) (title, webpageURL, duration, thumbnail, uploader string, err error) {
	// Add a 45-second timeout for the search so it doesn't hang forever, but gives yt-dlp enough time to parse cookies
	searchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	baseArgs := []string{
		"--force-ipv4",
		"--no-download",
		"--ignore-no-formats-error", // IMPORTANT: Skip format extraction errors for YouTube
		"--retries", "5",
		"--print", "%(title)s\n%(webpage_url)s\n%(duration_string)s\n%(thumbnail)s\n%(uploader)s",
		"--no-warnings",
		"--no-playlist",
	}

	args := BuildYtDlpArgs(guildID, baseArgs, "")

	args = append(args, query)
	cmd := exec.CommandContext(searchCtx, "yt-dlp", args...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("[AFK-BOT] [%s] [AUDIO] yt-dlp search error: %s", guildID, string(exitErr.Stderr))
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

// getTrackInfoMany extracts metadata for multiple tracks using a safe delimiter.
func getTrackInfoMany(ctx context.Context, guildID, query string) ([]SearchResult, error) {
	searchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	baseArgs := []string{
		"--force-ipv4",
		"--no-download",
		"--ignore-no-formats-error",
		"--retries", "5",
		"--print", "%(title)s|||%(webpage_url)s|||%(duration_string)s|||%(thumbnail)s|||%(uploader)s",
		"--no-warnings",
		"--no-playlist",
	}

	args := BuildYtDlpArgs(guildID, baseArgs, "")
	args = append(args, query)
	
	cmd := exec.CommandContext(searchCtx, "yt-dlp", args...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("[AFK-BOT] [%s] [AUDIO] yt-dlp search error: %s", guildID, string(exitErr.Stderr))
		}
		return nil, err
	}

	var results []SearchResult
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		parts := strings.Split(line, "|||")
		if len(parts) >= 2 {
			res := SearchResult{
				Title: strings.TrimSpace(parts[0]),
				Query: strings.TrimSpace(parts[1]),
			}
			
			if len(parts) >= 5 {
				res.Duration = strings.TrimSpace(parts[2])
				res.Thumbnail = strings.TrimSpace(parts[3])
				res.Uploader = strings.TrimSpace(parts[4])
			}
			
			if res.Title != "" && res.Query != "" {
				results = append(results, res)
			}
		}
	}
	
	if len(results) == 0 {
		return nil, fmt.Errorf("no results found")
	}
	
	return results, nil
}
