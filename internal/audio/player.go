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

	"github.com/jonas747/ogg"
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
	// Add a 15-second timeout for the search so it doesn't hang forever
	searchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	args := []string{
		"--force-ipv4",
		"--no-download",
		"--retries", "5",
		"--extractor-args", "youtube:player_client=tv,android,web",
		"--print", "%(title)s\n%(webpage_url)s\n%(duration_string)s\n%(thumbnail)s\n%(uploader)s",
		"--no-warnings",
		"--no-playlist",
	}

	if _, err := os.Stat("www.youtube.com_cookies.txt"); err == nil {
		args = append(args, "--cookies", "www.youtube.com_cookies.txt")
		log.Printf("[AFK-BOT] [AUDIO] Using www.youtube.com_cookies.txt for search...")
	} else if _, err := os.Stat("cookies.txt"); err == nil {
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

// opusFrameSize is the number of bytes in a 20ms PCM frame at 48kHz stereo 16-bit.
// 48000 samples/sec * 2 channels * 2 bytes/sample * 0.020 sec = 3840 bytes
const opusFrameSize = 3840

// StreamProvider wraps an ffmpeg process and reads raw Opus frames from it.
// It implements voice.OpusFrameProvider for disgo.
type StreamProvider struct {
	ffmpegCmd   *exec.Cmd
	stdout      io.ReadCloser
	decoder     *ogg.PacketDecoder
	skipPackets int
	filePath    string // temporary downloaded file to clean up
	done        chan error
	closeOnce   sync.Once
}

// ProvideOpusFrame reads the next opus frame.
func (p *StreamProvider) ProvideOpusFrame() ([]byte, error) {
	for {
		packet, _, err := p.decoder.Decode()
		if err != nil {
			select {
			case p.done <- err:
			default:
			}
			return nil, err
		}

		// The first 2 packets in an Ogg Opus stream are metadata (OpusHead, OpusTags)
		if p.skipPackets > 0 {
			p.skipPackets--
			continue
		}

		return packet, nil
	}
}

func (p *StreamProvider) Close() {
	p.closeOnce.Do(func() {
		log.Println("[AFK-BOT] [AUDIO] Closing StreamProvider...")
		if p.ffmpegCmd != nil && p.ffmpegCmd.Process != nil {
			p.ffmpegCmd.Process.Kill()
			p.ffmpegCmd.Wait()
		}
		if p.stdout != nil {
			p.stdout.Close()
		}
		if p.filePath != "" {
			os.Remove(p.filePath)
			log.Printf("[AFK-BOT] [AUDIO] Deleted temporary file: %s", p.filePath)
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
	log.Printf("[AFK-BOT] [AUDIO] Starting download for URL: %s", query)

	// Step 1: Download the audio file using yt-dlp
	tmpPrefix := filepath.Join(os.TempDir(), fmt.Sprintf("afk_audio_%d", time.Now().UnixNano()))

	args := []string{
		"--force-ipv4",
		"-f", "bestaudio",
		"--retries", "5",
		"--fragment-retries", "5",
		"--extractor-args", "youtube:player_client=tv,android,web",
		"-o", tmpPrefix + ".%(ext)s",
		"--no-playlist",
	}

	if _, err := os.Stat("www.youtube.com_cookies.txt"); err == nil {
		args = append(args, "--cookies", "www.youtube.com_cookies.txt")
		log.Printf("[AFK-BOT] [AUDIO] Using www.youtube.com_cookies.txt for download...")
	} else if _, err := os.Stat("cookies.txt"); err == nil {
		args = append(args, "--cookies", "cookies.txt")
		log.Printf("[AFK-BOT] [AUDIO] Using cookies.txt for download...")
	}

	args = append(args, query)
	ytCmd := exec.Command("yt-dlp", args...)

	log.Println("[AFK-BOT] [AUDIO] Downloading audio file...")
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
	log.Printf("[AFK-BOT] [AUDIO] Successfully downloaded to %s (%d bytes)", downloadedFile, stat.Size())

	// Step 2: Start ffmpeg to output OGG/Opus format for Discord
	// Discord requires: Opus codec, 48kHz, stereo
	// We use OGG container so we can extract individual opus packets reliably
	ffmpegCmd := exec.Command("ffmpeg",
		"-i", downloadedFile,
		"-vn",               // no video
		"-c:a", "libopus",   // encode to opus
		"-b:a", "96k",       // 96kbps bitrate
		"-ar", "48000",      // 48kHz sample rate
		"-ac", "2",          // stereo
		"-frame_duration", "20", // 20ms frames (Discord standard)
		"-application", "audio",
		"-vbr", "on",
		"-compression_level", "10",
		"-f", "ogg",         // OGG container output
		"-page_duration", "20000", // Force 1 packet per page (20ms = 20000us)
		"-flush_packets", "1",
		"-loglevel", "warning",
		"pipe:1",            // output to stdout
	)

	// Capture ffmpeg stderr for debugging
	ffmpegCmd.Stderr = os.Stderr

	stdout, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		os.Remove(downloadedFile)
		return nil, fmt.Errorf("failed to create ffmpeg stdout pipe: %w", err)
	}

	log.Println("[AFK-BOT] [AUDIO] Starting ffmpeg encode (OGG/Opus)...")
	if err := ffmpegCmd.Start(); err != nil {
		os.Remove(downloadedFile)
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	decoder := ogg.NewPacketDecoder(ogg.NewDecoder(stdout))

	provider := &StreamProvider{
		ffmpegCmd:   ffmpegCmd,
		stdout:      stdout,
		decoder:     decoder,
		skipPackets: 2, // Skip OpusHead and OpusTags
		filePath:    downloadedFile,
		done:        make(chan error, 1),
	}

	// Wait for ffmpeg in background to avoid zombies
	go func() {
		err := ffmpegCmd.Wait()
		if err != nil {
			log.Printf("[AUDIO] ffmpeg process exited with error: %v", err)
		} else {
			log.Println("[AUDIO] ffmpeg process completed successfully")
		}
	}()

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
