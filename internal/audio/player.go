package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jonas747/dca"
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
		// For direct URLs, just get the title
		title, _, duration, thumb, uploader, err := getTrackInfo(ctx, query)
		if err != nil {
			if strings.Contains(query, "youtube.com") || strings.Contains(query, "youtu.be") {
				return nil, fmt.Errorf("YOUTUBE_BLOCKED")
			}
			return nil, err
		}
		return &SearchResult{
			Title: title, Query: query, Duration: duration, Thumbnail: thumb, Uploader: uploader,
		}, nil
	}

	// Try YouTube search first
	title, webpageURL, duration, thumb, uploader, err := getTrackInfo(ctx, fmt.Sprintf("ytsearch:%s", query))
	if err == nil {
		return &SearchResult{
			Title: title, Query: webpageURL, Duration: duration, Thumbnail: thumb, Uploader: uploader,
		}, nil
	}

	// Fallback to SoundCloud
	log.Printf("[AUDIO] YouTube search failed. Fallback to SoundCloud: %s", query)
	title, webpageURL, duration, thumb, uploader, err = getTrackInfo(ctx, fmt.Sprintf("scsearch:%s", query))
	if err != nil {
		return nil, fmt.Errorf("failed to find track on YouTube or SoundCloud: %w", err)
	}
	return &SearchResult{
		Title: title, Query: webpageURL, Duration: duration, Thumbnail: thumb, Uploader: uploader,
	}, nil
}

// getTrackInfo extracts the metadata from yt-dlp without downloading.
func getTrackInfo(ctx context.Context, query string) (title, webpageURL, duration, thumbnail, uploader string, err error) {
	// Add a 15-second timeout for the search so it doesn't hang forever
	searchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(searchCtx, "yt-dlp",
		"--force-ipv4",
		"--no-download",
		"--retries", "5",
		"--extractor-args", "youtube:player_client=android", // Attempt to bypass YouTube bot block
		"--print", "%(title)s\n%(webpage_url)s\n%(duration_string)s\n%(thumbnail)s\n%(uploader)s",
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
		return "", "", "", "", "", err
	}
	
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		// Sometimes yt-dlp might only return title if webpage_url is missing
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

// StreamProvider wraps a dca.EncodeSession to implement voice.OpusFrameProvider
// and adds a Done channel to signal when the stream ends.
type StreamProvider struct {
	Session   *dca.EncodeSession
	filePath  string // temporary downloaded file
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
		if p.filePath != "" {
			os.Remove(p.filePath)
			log.Printf("[AUDIO] Deleted temporary file: %s", p.filePath)
		}
	})
}

// WaitDone returns a channel that receives an error when the stream ends.
func (p *StreamProvider) WaitDone() <-chan error {
	return p.done
}

// NewStream creates a new audio stream by downloading the audio to a temporary file,
// then encoding it to Opus via dca/ffmpeg. This is the most stable method and avoids
// all streaming/piping bugs with ffmpeg.
func NewStream(query string) (*StreamProvider, error) {
	log.Printf("[AUDIO] Starting download for URL: %s", query)

	// Generate a unique temporary file prefix
	tmpPrefix := filepath.Join(os.TempDir(), fmt.Sprintf("afk_audio_%d", time.Now().UnixNano()))
	
	// Start yt-dlp to download the audio
	ytCmd := exec.Command("yt-dlp",
		"--force-ipv4",
		"-f", "bestaudio",
		"--retries", "5",
		"--fragment-retries", "5",
		"--extractor-args", "youtube:player_client=android", // bypass youtube block
		"-o", tmpPrefix+".%(ext)s",
		"--no-playlist",
		query,
	)
	
	log.Println("[AUDIO] Downloading audio file...")
	out, err := ytCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to download audio: %w, output: %s", err, string(out))
	}

	// yt-dlp appends the format extension automatically, so we must search for the file
	files, err := filepath.Glob(tmpPrefix + ".*")
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("downloaded file not found after yt-dlp success")
	}
	
	downloadedFile := files[0]
	log.Printf("[AUDIO] Successfully downloaded to %s", downloadedFile)

	// Configure DCA encoding options for Discord
	opts := dca.StdEncodeOptions
	opts.RawOutput = true    // raw Opus frames, no DCA container
	opts.Bitrate = 96        // 96kbps audio
	opts.Application = "audio"
	opts.BufferedFrames = 200 // larger buffer for stability
	
	log.Println("[AUDIO] Starting DCA/ffmpeg encode session from file...")
	encodeSession, err := dca.EncodeFile(downloadedFile, opts)
	if err != nil {
		os.Remove(downloadedFile) // cleanup if encode fails
		return nil, fmt.Errorf("failed to create dca encode session: %w", err)
	}

	provider := &StreamProvider{
		Session:  encodeSession,
		filePath: downloadedFile,
		done:     make(chan error, 1),
	}

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
