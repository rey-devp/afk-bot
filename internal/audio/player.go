package audio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/jonas747/dca"
)

// SearchAndExtract gets the direct audio URL from a query (YouTube search or URL).
func SearchAndExtract(ctx context.Context, query string) (title string, url string, err error) {
	// First, check if it's already a URL
	isURL := strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://")

	var cmd *exec.Cmd
	if isURL {
		// Use URL directly
		cmd = exec.CommandContext(ctx, "yt-dlp", "-f", "bestaudio", "-e", "-g", "--no-playlist", query)
	} else {
		// Search on YouTube
		searchQuery := fmt.Sprintf("ytsearch:%s", query)
		cmd = exec.CommandContext(ctx, "yt-dlp", "-f", "bestaudio", "-e", "-g", "--no-playlist", searchQuery)
	}

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("[AUDIO] yt-dlp error: %s", string(exitErr.Stderr))
		}
		return "", "", fmt.Errorf("failed to extract audio: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) >= 2 {
		title = lines[0]
		url = lines[1]
		return title, url, nil
	}

	return "", "", errors.New("failed to parse yt-dlp output")
}

type StreamProvider struct {
	Session *dca.EncodeSession
	Done    chan error
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
	p.Session.Cleanup()
}

// NewOpusStream creates a new audio stream for Discord from a direct URL using DCA.
func NewOpusStream(url string) (*StreamProvider, error) {
	options := dca.StdEncodeOptions
	options.RawOutput = true
	options.Bitrate = 96
	options.Application = "audio"

	encodeSession, err := dca.EncodeFile(url, options)
	if err != nil {
		return nil, err
	}

	provider := &StreamProvider{
		Session: encodeSession,
		Done:    make(chan error, 1),
	}
	
	return provider, nil
}
