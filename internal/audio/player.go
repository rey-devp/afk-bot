package audio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/jonas747/dca"
)

// SearchAndExtract gets the direct audio URL from a query (YouTube search or URL).
func SearchAndExtract(ctx context.Context, query string) (title string, url string, err error) {
	isURL := strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://")

	// Try extracting with the given query (or ytsearch)
	title, url, err = executeYtDlp(ctx, query, isURL, "ytsearch:")
	if err == nil {
		return title, url, nil
	}

	// If it failed and it's NOT a direct URL, fallback to SoundCloud search
	if !isURL {
		log.Printf("[AUDIO] YouTube search failed or blocked. Fallback to SoundCloud: %s", query)
		title, url, err = executeYtDlp(ctx, query, isURL, "scsearch:")
		if err == nil {
			return title, url, nil
		}
	}

	return "", "", err
}

func executeYtDlp(ctx context.Context, query string, isURL bool, searchPrefix string) (string, string, error) {
	var cmd *exec.Cmd
	// --force-ipv4 sometimes helps bypass IPv6 datacenter blocks on YouTube
	if isURL {
		cmd = exec.CommandContext(ctx, "yt-dlp", "--force-ipv4", "--print", "%(title)s\n%(webpage_url)s", "--no-warnings", "--no-playlist", query)
	} else {
		searchQuery := fmt.Sprintf("%s%s", searchPrefix, query)
		cmd = exec.CommandContext(ctx, "yt-dlp", "--force-ipv4", "--print", "%(title)s\n%(webpage_url)s", "--no-warnings", "--no-playlist", searchQuery)
	}

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("[AUDIO] yt-dlp extraction error: %s", string(exitErr.Stderr))
		}
		return "", "", fmt.Errorf("failed to extract audio: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) >= 2 {
		return lines[0], lines[1], nil
	}

	return "", "", errors.New("failed to parse yt-dlp output")
}

type StreamProvider struct {
	Session *dca.EncodeSession
	Done    chan error
	YtCmd   *exec.Cmd
}

func (p *StreamProvider) ProvideOpusFrame() ([]byte, error) {
	frame, err := p.Session.OpusFrame()
	if err != nil {
		select {
		case p.Done <- err:
		default:
		}
		return nil, err
	}
	return frame, nil
}

func (p *StreamProvider) Close() {
	if p.YtCmd != nil && p.YtCmd.Process != nil {
		p.YtCmd.Process.Kill()
	}
	p.Session.Cleanup()
}

// NewOpusStream creates a new audio stream for Discord from a direct URL using DCA.
func NewOpusStream(url string) (*StreamProvider, error) {
	options := dca.StdEncodeOptions
	opts := *options
	opts.RawOutput = true
	opts.Bitrate = 96
	opts.Application = "audio"

	// Run yt-dlp to download and pipe to stdout
	log.Printf("[AUDIO] Starting yt-dlp stream for: %s", url)
	ytCmd := exec.Command("yt-dlp", "--force-ipv4", "-q", "--no-warnings", "-f", "bestaudio", "-o", "-", url)
	
	// Optimize logs: pipe stderr to bot's console so we can see if YouTube blocks the download
	ytCmd.Stderr = os.Stderr
	
	stdout, err := ytCmd.StdoutPipe()
	if err != nil {
		log.Printf("[AUDIO] Failed to create stdout pipe: %v", err)
		return nil, err
	}

	if err := ytCmd.Start(); err != nil {
		return nil, err
	}

	encodeSession, err := dca.EncodeMem(stdout, &opts)
	if err != nil {
		ytCmd.Process.Kill()
		return nil, err
	}

	provider := &StreamProvider{
		Session: encodeSession,
		Done:    make(chan error, 1),
		YtCmd:   ytCmd,
	}
	
	// Clean up yt-dlp zombie process when it finishes
	go func() {
		ytCmd.Wait()
	}()

	return provider, nil
}
