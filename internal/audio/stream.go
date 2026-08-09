package audio

import (
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

var youtubePlayerClients = []string{"ios", "android"}

var (
	ytCooldownMap sync.Map // Map guildID string -> time.Time
)

// CheckYtCooldown returns true if the guild is currently in cooldown.
func CheckYtCooldown(guildID string) (bool, time.Duration) {
	if val, ok := ytCooldownMap.Load(guildID); ok {
		expiresAt := val.(time.Time)
		if time.Now().Before(expiresAt) {
			return true, time.Until(expiresAt)
		}
		ytCooldownMap.Delete(guildID)
	}
	return false, 0
}

// SetYtCooldown sets a 60-second cooldown for the guild.
func SetYtCooldown(guildID string) {
	ytCooldownMap.Store(guildID, time.Now().Add(60*time.Second))
	log.Printf("[AFK-BOT] [%s] [AUDIO] Bot detection triggered, entering 60s cooldown for guild", guildID)
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
func NewStream(guildID string, query string) (*StreamProvider, error) {
	log.Printf("[AFK-BOT] [%s] [AUDIO] Starting download for URL: %s", guildID, query)

	// Step 1: Download the audio file using yt-dlp
	tmpPrefix := filepath.Join(os.TempDir(), fmt.Sprintf("afk_audio_%d", time.Now().UnixNano()))

	baseArgs := []string{
		"--force-ipv4",
		"-f", "bestaudio/best",
		"--retries", "5",
		"--fragment-retries", "5",
		"-o", tmpPrefix + ".%(ext)s",
		"--no-playlist",
	}

	var downloadedFile string
	var lastErr error

	isYouTube := strings.Contains(query, "youtube.com") || strings.Contains(query, "youtu.be") || strings.HasPrefix(query, "ytsearch")

	if isYouTube {
		if inCooldown, remaining := CheckYtCooldown(guildID); inCooldown {
			log.Printf("[AFK-BOT] [%s] [AUDIO] Guild still in bot-detection cooldown (%ds remaining), skipping YouTube attempt", guildID, int(remaining.Seconds()))
			return nil, fmt.Errorf("YOUTUBE_BLOCKED_COOLDOWN: Sign in to confirm you're not a bot")
		}
	}

	clientsToTry := youtubePlayerClients
	if !isYouTube {
		clientsToTry = []string{""} // SoundCloud doesn't need specific player clients or retries
	}

	for i, client := range clientsToTry {
		if client != "" {
			log.Printf("[AFK-BOT] [%s] [AUDIO] Attempt %d/%d using player_client=%s", guildID, i+1, len(clientsToTry), client)
		}

		args := BuildYtDlpArgs(guildID, baseArgs, client)
		args = append(args, query)

		ytCmd := exec.Command("yt-dlp", args...)
		out, err := ytCmd.CombinedOutput()

		if err == nil {
			// Success! Find the file.
			files, errGlob := filepath.Glob(tmpPrefix + ".*")
			if errGlob == nil && len(files) > 0 {
				downloadedFile = files[0]
				stat, errStat := os.Stat(downloadedFile)
				if errStat == nil && stat.Size() > 0 {
					log.Printf("[AFK-BOT] [%s] [AUDIO] Successfully downloaded to %s (%d bytes) with player_client=%s", guildID, downloadedFile, stat.Size(), client)
					lastErr = nil
					break // Break out of the client loop
				}
			}
			lastErr = fmt.Errorf("download succeeded but file was missing or empty")
			log.Printf("[AFK-BOT] [%s] [AUDIO] player_client=%s failed: %v", guildID, client, lastErr)
		} else {
			outStr := string(out)
			lastErr = fmt.Errorf("player_client=%s failed: %w, output: %s", client, err, outStr)
			
			if strings.Contains(outStr, "Sign in to confirm you") || strings.Contains(outStr, "Sign in to confirm you're not a bot") {
				log.Printf("[AFK-BOT] [%s] [AUDIO] Bot detection triggered on player_client=%s, stopping further client attempts for this URL", guildID, client)
				SetYtCooldown(guildID)
				return nil, lastErr
			}
			
			log.Printf("[AFK-BOT] [%s] [AUDIO] Download failed: %v", guildID, err)
		}

		if i < len(clientsToTry)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	if lastErr != nil {
		if !isYouTube {
			return nil, fmt.Errorf("download failed: %w", lastErr)
		}
		return nil, fmt.Errorf("all player clients failed, last error: %w", lastErr)
	}

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

	log.Printf("[AFK-BOT] [%s] [AUDIO] Starting ffmpeg encode (OGG/Opus)...", guildID)
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
			log.Printf("[AFK-BOT] [%s] [AUDIO] ffmpeg process exited with error: %v", guildID, err)
		} else {
			log.Printf("[AFK-BOT] [%s] [AUDIO] ffmpeg process completed successfully", guildID)
		}
	}()

	log.Printf("[AFK-BOT] [%s] [AUDIO] Stream pipeline ready!", guildID)
	return provider, nil
}
