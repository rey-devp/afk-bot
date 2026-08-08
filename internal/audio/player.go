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
	ffmpegCmd *exec.Cmd
	stdout    io.ReadCloser
	filePath  string // temporary downloaded file to clean up
	done      chan error
	closeOnce sync.Once
	
	packetBuf [][]byte // buffer for packets inside a single OGG page
}

// ProvideOpusFrame reads the next opus frame from the ffmpeg ogg/opus output.
func (p *StreamProvider) ProvideOpusFrame() ([]byte, error) {
	// If we have buffered packets, return the next one
	if len(p.packetBuf) > 0 {
		pkt := p.packetBuf[0]
		p.packetBuf = p.packetBuf[1:]
		return pkt, nil
	}

	// Buffer is empty, read the next OGG page
	for {
		page, err := readOggPage(p.stdout)
		if err != nil {
			select {
			case p.done <- err:
			default:
			}
			return nil, err
		}

		if page.isHeader || len(page.packets) == 0 {
			continue // Skip headers or empty pages
		}

		// Fill the buffer
		p.packetBuf = page.packets
		
		// Pop the first packet
		pkt := p.packetBuf[0]
		p.packetBuf = p.packetBuf[1:]
		return pkt, nil
	}
}

func (p *StreamProvider) Close() {
	p.closeOnce.Do(func() {
		log.Println("[AUDIO] Closing StreamProvider...")
		if p.ffmpegCmd != nil && p.ffmpegCmd.Process != nil {
			p.ffmpegCmd.Process.Kill()
			p.ffmpegCmd.Wait()
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
// 2. Using ffmpeg to convert the file to PCM audio
// 3. Encoding PCM to Opus packets on the fly
//
// This approach is the most robust because:
// - yt-dlp handles all the auth/DRM/cookies negotiation
// - ffmpeg reads a local file (no network issues)
// - We have full control over the audio pipeline
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

	// Step 2: Start ffmpeg to output OGG/Opus format for Discord
	// Discord requires: Opus codec, 48kHz, stereo
	// We use OGG container so we can extract individual opus packets
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

	log.Println("[AUDIO] Starting ffmpeg encode (OGG/Opus)...")
	if err := ffmpegCmd.Start(); err != nil {
		os.Remove(downloadedFile)
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Give ffmpeg a moment to initialize and start producing data
	time.Sleep(500 * time.Millisecond)

	provider := &StreamProvider{
		ffmpegCmd: ffmpegCmd,
		stdout:    stdout,
		filePath:  downloadedFile,
		done:      make(chan error, 1),
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

// This reads raw Opus packets from an OGG container stream.
// OGG pages have a specific structure: "OggS" magic, then header fields, then segments.

type oggPage struct {
	isHeader bool
	packets  [][]byte
}

func readOggPage(r io.Reader) (*oggPage, error) {
	// Read OGG page header (27 bytes minimum)
	header := make([]byte, 27)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	// Verify magic "OggS"
	if string(header[0:4]) != "OggS" {
		return nil, fmt.Errorf("invalid OGG magic: %x", header[0:4])
	}

	// header[5] = page type flag
	// Bit 0x02 = beginning of stream (BOS) = header page
	pageType := header[5]
	isHeader := (pageType & 0x02) != 0

	// Granule position at bytes 6-13
	granulePos := uint64(header[6]) | uint64(header[7])<<8 | uint64(header[8])<<16 |
		uint64(header[9])<<24 | uint64(header[10])<<32 | uint64(header[11])<<40 |
		uint64(header[12])<<48 | uint64(header[13])<<56

	// If granule position is 0, it's likely a header page
	if granulePos == 0 {
		isHeader = true
	}

	// Number of page segments at byte 26
	numSegments := int(header[26])

	// Read the segment table
	segmentTable := make([]byte, numSegments)
	if _, err := io.ReadFull(r, segmentTable); err != nil {
		return nil, err
	}

	// Calculate total page data size
	totalSize := 0
	for _, s := range segmentTable {
		totalSize += int(s)
	}

	// Read all segment data
	data := make([]byte, totalSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	// Parse segments into packets
	// A packet ends when a segment length < 255 is encountered
	var packets [][]byte
	var currentPacket []byte
	offset := 0
	for _, segLen := range segmentTable {
		currentPacket = append(currentPacket, data[offset:offset+int(segLen)]...)
		offset += int(segLen)
		if segLen < 255 {
			// End of packet
			if len(currentPacket) > 0 {
				pkt := make([]byte, len(currentPacket))
				copy(pkt, currentPacket)
				packets = append(packets, pkt)
			}
			currentPacket = currentPacket[:0]
		}
	}
	// If there's remaining data (packet spans pages), include it
	if len(currentPacket) > 0 {
		pkt := make([]byte, len(currentPacket))
		copy(pkt, currentPacket)
		packets = append(packets, pkt)
	}

	return &oggPage{
		isHeader: isHeader,
		packets:  packets,
	}, nil
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
