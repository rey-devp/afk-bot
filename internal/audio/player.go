package audio

import (
	"context"
	"errors"
	"fmt"
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
			return nil, err
		}
		return &SearchResult{
			Title: title, Query: query, Duration: duration, Thumbnail: thumb, Uploader: uploader,
		}, nil
	}

	// Always use SoundCloud search to avoid YouTube bot restrictions
	log.Printf("[AUDIO] Searching SoundCloud: %s", query)
	title, webpageURL, duration, thumb, uploader, err := getTrackInfo(ctx, fmt.Sprintf("scsearch:%s", query))
	if err != nil {
		return nil, fmt.Errorf("failed to find track on SoundCloud: %w", err)
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
		"--extractor-args", "youtube:player_client=android",
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

// opusFrameSize is the number of bytes in a 20ms PCM frame at 48kHz stereo 16-bit.
// 48000 samples/sec * 2 channels * 2 bytes/sample * 0.020 sec = 3840 bytes
const opusFrameSize = 3840

// StreamProvider wraps an ffmpeg process and reads raw Opus frames from it.
// It implements voice.OpusFrameProvider for disgo.
type StreamProvider struct {
	session   *dca.EncodeSession
	filePath  string // temporary downloaded file to clean up
	done      chan error
	closeOnce sync.Once
}

// ProvideOpusFrame reads the next opus frame.
func (p *StreamProvider) ProvideOpusFrame() ([]byte, error) {
	frame, err := p.session.OpusFrame()
	if err != nil {
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
		if p.session != nil {
			p.session.Cleanup()
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

// NewStream creates a new audio stream by:
// 1. Downloading the audio using yt-dlp to a temporary file
// 2. Using dca to accurately encode the file to Opus frames
func NewStream(query string) (*StreamProvider, error) {
	log.Printf("[AUDIO] Starting download for URL: %s", query)

	// Step 1: Download the audio file using yt-dlp
	tmpPrefix := filepath.Join(os.TempDir(), fmt.Sprintf("afk_audio_%d", time.Now().UnixNano()))

	ytCmd := exec.Command("yt-dlp",
		"--force-ipv4",
		"-f", "bestaudio",
		"--retries", "5",
		"--fragment-retries", "5",
		"--extractor-args", "youtube:player_client=android",
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

	// Verify the file has content
	stat, err := os.Stat(downloadedFile)
	if err != nil || stat.Size() == 0 {
		os.Remove(downloadedFile)
		return nil, fmt.Errorf("downloaded file is empty or unreadable")
	}
	log.Printf("[AUDIO] Successfully downloaded to %s (%d bytes)", downloadedFile, stat.Size())

	// Step 2: Start dca encoding
	log.Println("[AUDIO] Starting dca encode (Opus)...")
	opts := dca.StdEncodeOptions
	opts.RawOutput = true
	opts.Bitrate = 96
	opts.Application = dca.AudioApplicationAudio
	opts.VBR = true

	encodeSession, err := dca.EncodeFile(downloadedFile, opts)
	if err != nil {
		os.Remove(downloadedFile)
		return nil, fmt.Errorf("failed to start dca encode: %w", err)
	}

	provider := &StreamProvider{
		session:   encodeSession,
		filePath:  downloadedFile,
		done:      make(chan error, 1),
	}

	log.Println("[AUDIO] Stream pipeline ready!")
	return provider, nil
}



// Legacy wrappers for backwards compatibility
func SearchAndExtract(ctx context.Context, query string) (string, string, error) {
	result, err := Search(ctx, query)
	if err != nil {
		return "", "", err
	}
	return result.Title, result.Query, nil
}

func NewOpusStream(query string) (*StreamProvider, error) {
	return NewStream(query)
}

func (p *StreamProvider) DoneChan() <-chan error {
	return p.done
}
