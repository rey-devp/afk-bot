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
// It returns the title and the original query (which yt-dlp can use to download later).
func Search(ctx context.Context, query string) (*SearchResult, error) {
	isURL := strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://")

	if isURL {
		// For direct URLs, just get the title
		title, err := getTitle(ctx, query)
		if err != nil {
			return nil, err
		}
		return &SearchResult{Title: title, Query: query}, nil
	}

	// Try YouTube search first
	title, err := getTitle(ctx, fmt.Sprintf("ytsearch:%s", query))
	if err == nil {
		// Use the same ytsearch query for download so yt-dlp resolves it again
		return &SearchResult{Title: title, Query: fmt.Sprintf("ytsearch:%s", query)}, nil
	}

	// Fallback to SoundCloud
	log.Printf("[AUDIO] YouTube search failed. Fallback to SoundCloud: %s", query)
	title, err = getTitle(ctx, fmt.Sprintf("scsearch:%s", query))
	if err != nil {
		return nil, fmt.Errorf("failed to find track on YouTube or SoundCloud: %w", err)
	}
	return &SearchResult{Title: title, Query: fmt.Sprintf("scsearch:%s", query)}, nil
}

// getTitle extracts just the title from yt-dlp without downloading.
func getTitle(ctx context.Context, query string) (string, error) {
	// Add a 15-second timeout for the search so it doesn't hang forever
	searchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(searchCtx, "yt-dlp",
		"--force-ipv4",
		"--no-download",
		"--print", "%(title)s",
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
		return "", err
	}
	title := strings.TrimSpace(string(out))
	if title == "" {
		return "", fmt.Errorf("yt-dlp returned empty title")
	}
	return title, nil
}

// StreamProvider wraps a dca.EncodeSession to implement voice.OpusFrameProvider
// and adds a Done channel to signal when the stream ends.
type StreamProvider struct {
	Session  *dca.EncodeSession
	ytCmd    *exec.Cmd
	done     chan error
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

// NewStream creates a new audio stream by running yt-dlp to download
// and pipe audio, then encoding it to Opus via dca/ffmpeg.
// The query can be "ytsearch:...", "scsearch:...", or a direct URL.
func NewStream(query string) (*StreamProvider, error) {
	log.Printf("[AUDIO] Starting stream for: %s", query)

	// Configure DCA encoding options for Discord
	opts := *dca.StdEncodeOptions
	opts.RawOutput = true    // raw Opus frames, no DCA container
	opts.Bitrate = 96        // 96kbps audio
	opts.Application = "audio"
	opts.BufferedFrames = 200 // larger buffer for stability

	// Run yt-dlp: download best audio and pipe raw bytes to stdout
	ytCmd := exec.Command("yt-dlp",
		"--force-ipv4",
		"-f", "bestaudio",
		"-o", "-",          // output to stdout
		"-q",               // quiet mode (no progress bar text into pipe)
		"--no-warnings",
		"--no-playlist",
		query,
	)
	ytCmd.Stderr = os.Stderr // log yt-dlp errors to console

	stdout, err := ytCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create yt-dlp stdout pipe: %w", err)
	}

	log.Println("[AUDIO] Starting yt-dlp process...")
	if err := ytCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start yt-dlp: %w", err)
	}

	// Give yt-dlp a moment to start producing data
	// This helps avoid dca/ffmpeg getting an empty pipe
	time.Sleep(2 * time.Second)

	log.Println("[AUDIO] Starting DCA/ffmpeg encode session...")
	encodeSession, err := dca.EncodeMem(stdout, &opts)
	if err != nil {
		ytCmd.Process.Kill()
		return nil, fmt.Errorf("failed to create dca encode session: %w", err)
	}

	provider := &StreamProvider{
		Session: encodeSession,
		ytCmd:   ytCmd,
		done:    make(chan error, 1),
	}

	// Goroutine to clean up yt-dlp when it exits
	go func() {
		err := ytCmd.Wait()
		if err != nil {
			log.Printf("[AUDIO] yt-dlp process exited with: %v", err)
		} else {
			log.Println("[AUDIO] yt-dlp process finished successfully")
		}
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
