package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jonas747/dca"
)

// SearchResult holds the metadata found by yt-dlp search (title only).
type SearchResult struct {
	Title string
	Query string // original query or resolved URL for downloading
}

// Search finds a track using yt-dlp but does NOT download it.
// It returns the title and the resolved webpage URL (which yt-dlp can use to extract media later).
func Search(ctx context.Context, query string) (*SearchResult, error) {
	isURL := strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://")

	if isURL {
		// For direct URLs, just get the title
		title, _, err := getTrackInfo(ctx, query)
		if err != nil {
			if strings.Contains(query, "youtube.com") || strings.Contains(query, "youtu.be") {
				return nil, fmt.Errorf("YOUTUBE_BLOCKED")
			}
			return nil, err
		}
		// If it's already a URL, just use it directly
		return &SearchResult{Title: title, Query: query}, nil
	}

	// Try YouTube search first
	title, webpageURL, err := getTrackInfo(ctx, fmt.Sprintf("ytsearch:%s", query))
	if err == nil {
		return &SearchResult{Title: title, Query: webpageURL}, nil
	}

	// Fallback to SoundCloud
	log.Printf("[AUDIO] YouTube search failed. Fallback to SoundCloud: %s", query)
	title, webpageURL, err = getTrackInfo(ctx, fmt.Sprintf("scsearch:%s", query))
	if err != nil {
		return nil, fmt.Errorf("failed to find track on YouTube or SoundCloud: %w", err)
	}
	return &SearchResult{Title: title, Query: webpageURL}, nil
}

// getTrackInfo extracts the title and webpage URL from yt-dlp without downloading.
func getTrackInfo(ctx context.Context, query string) (title string, webpageURL string, err error) {
	// Add a 15-second timeout for the search so it doesn't hang forever
	searchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(searchCtx, "yt-dlp",
		"--force-ipv4",
		"--no-download",
		"--extractor-args", "youtube:player_client=android", // Attempt to bypass YouTube bot block
		"--print", "%(title)s\n%(webpage_url)s",
		"--no-warnings",
		"--no-playlist",
		query,
	)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("[AUDIO] yt-dlp search error: %s", string(exitErr.Stderr))
		}
		return "", "", err
	}
	
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		// Sometimes yt-dlp might only return title if webpage_url is missing
		if len(lines) == 1 && lines[0] != "" {
			return lines[0], query, nil
		}
		return "", "", fmt.Errorf("yt-dlp returned incomplete info")
	}
	
	title = strings.TrimSpace(lines[0])
	webpageURL = strings.TrimSpace(lines[1])
	
	if title == "" || webpageURL == "" {
		return "", "", fmt.Errorf("yt-dlp returned empty title or URL")
	}
	return title, webpageURL, nil
}

// StreamProvider wraps a dca.EncodeSession to implement voice.OpusFrameProvider
// and adds a Done channel to signal when the stream ends.
type StreamProvider struct {
	Session   *dca.EncodeSession
	ytCmd     *exec.Cmd
	done      chan error
	closeOnce sync.Once
}

func (p *StreamProvider) ProvideOpusFrame() ([]byte, error) {
	frame, err := p.Session.OpusFrame()
	if err != nil {
		// Signal that the stream is done (EOF or error)
		select {
		case p.done <- err:
		default:
		}
		return nil, err
	}
	return frame, nil
}

func (p *StreamProvider) Close() {
	p.closeOnce.Do(func() {
		log.Println("[AUDIO] Closing StreamProvider...")
		if p.Session != nil {
			p.Session.Cleanup()
		}
		if p.ytCmd != nil && p.ytCmd.Process != nil {
			p.ytCmd.Process.Kill()
		}
	})
}

// WaitDone returns a channel that receives an error when the stream ends.
func (p *StreamProvider) WaitDone() <-chan error {
	return p.done
}

// NewStream creates a new audio stream by piping yt-dlp directly to dca/ffmpeg.
// The query is guaranteed to be a resolved webpage URL (from Search).
func NewStream(query string) (*StreamProvider, error) {
	log.Printf("[AUDIO] Starting stream for URL: %s", query)

	// Configure DCA encoding options for Discord
	opts := dca.StdEncodeOptions
	opts.RawOutput = true    // raw Opus frames, no DCA container
	opts.Bitrate = 96        // 96kbps audio
	opts.Application = "audio"
	opts.BufferedFrames = 200 // larger buffer for stability
	
	// Start yt-dlp to download the audio and pipe to stdout
	ytCmd := exec.Command("yt-dlp",
		"--force-ipv4",
		"-f", "bestaudio",
		"--extractor-args", "youtube:player_client=android", // bypass youtube block
		"-o", "-",          // output media to stdout
		"-q",               // quiet mode
		"--no-warnings",
		query,
	)
	
	// Send yt-dlp errors to bot's console for debugging
	ytCmd.Stderr = os.Stderr 

	stdout, err := ytCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create yt-dlp stdout pipe: %w", err)
	}

	log.Println("[AUDIO] Starting yt-dlp process...")
	if err := ytCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start yt-dlp: %w", err)
	}

	// Wait briefly for yt-dlp to negotiate and start streaming data
	time.Sleep(2 * time.Second)

	log.Println("[AUDIO] Starting DCA/ffmpeg encode session from pipe...")
	encodeSession, err := dca.EncodeMem(stdout, opts)
	if err != nil {
		ytCmd.Process.Kill()
		return nil, fmt.Errorf("failed to create dca encode session: %w", err)
	}

	provider := &StreamProvider{
		Session: encodeSession,
		ytCmd:   ytCmd, // keep reference to kill it later
		done:    make(chan error, 1),
	}

	// Wait for yt-dlp in background to avoid zombies
	go func() {
		_ = ytCmd.Wait()
		log.Println("[AUDIO] yt-dlp process finished")
	}()

	log.Println("[AUDIO] Stream pipeline ready!")
	return provider, nil
}

// Legacy wrapper for backwards compatibility
func SearchAndExtract(ctx context.Context, query string) (string, string, error) {
	result, err := Search(ctx, query)
	if err != nil {
		return "", "", err
	}
	return result.Title, result.Query, nil
}

// Legacy wrapper
func NewOpusStream(query string) (*StreamProvider, error) {
	return NewStream(query)
}

// WaitDoneCompat returns the done channel (for queue.go usage)
func (p *StreamProvider) DoneChan() <-chan error {
	return p.done
}

// GetEncodeSession returns the underlying dca.EncodeSession (for cleanup in queue)
func (p *StreamProvider) GetEncodeSession() *dca.EncodeSession {
	return p.Session
}

// ReadOpusFrames is a debug utility that counts how many frames are produced
func ReadOpusFrames(p *StreamProvider, maxFrames int) int {
	count := 0
	for i := 0; i < maxFrames; i++ {
		_, err := p.Session.OpusFrame()
		if err != nil {
			if err == io.EOF {
				log.Printf("[AUDIO] DEBUG: Read %d frames before EOF", count)
			} else {
				log.Printf("[AUDIO] DEBUG: Read %d frames before error: %v", count, err)
			}
			return count
		}
		count++
	}
	log.Printf("[AUDIO] DEBUG: Read %d frames (max reached)", count)
	return count
}
