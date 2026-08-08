package bot

import (
	"context"
	"io"
	"log"
	"sync"

	"bot-afk/internal/audio"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

type Track struct {
	Title       string
	Query       string // yt-dlp compatible query (e.g. "ytsearch:xxx" or direct URL)
	Duration    string
	Thumbnail   string
	Uploader    string
	RequestedBy snowflake.ID
}

type GuildQueue struct {
	GuildID      snowflake.ID
	Bot          *Bot
	Tracks       []Track
	CurrentTrack *Track
	mu           sync.Mutex
	isPlaying    bool
	isPaused     bool
	cancelPlay   context.CancelFunc
	stream       *audio.StreamProvider
}

func (q *GuildQueue) AddTrack(track Track) {
	q.mu.Lock()
	q.Tracks = append(q.Tracks, track)
	q.mu.Unlock()
	log.Printf("[QUEUE] Track added: %s (guild %s, queue length: %d)", track.Title, q.GuildID, len(q.Tracks))
}

func (q *GuildQueue) PlayNext() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.isPlaying {
		log.Printf("[QUEUE] Already playing in guild %s, skipping PlayNext", q.GuildID)
		return
	}

	if len(q.Tracks) == 0 {
		log.Printf("[QUEUE] No more tracks in guild %s, switching to silence", q.GuildID)
		q.CurrentTrack = nil
		// No more tracks, switch back to SilenceProvider to prevent AFK kick
		conn := q.Bot.Client.VoiceManager.GetConn(q.GuildID)
		if conn != nil {
			conn.SetOpusFrameProvider(&audio.SilenceProvider{})
		}
		return
	}

	// Pop the first track
	track := q.Tracks[0]
	q.Tracks = q.Tracks[1:]
	q.CurrentTrack = &track
	q.isPlaying = true

	ctx, cancel := context.WithCancel(context.Background())
	q.cancelPlay = cancel

	log.Printf("[QUEUE] Starting playback: %s in guild %s", track.Title, q.GuildID)
	go q.playRoutine(ctx, track)
}

func (q *GuildQueue) playRoutine(ctx context.Context, track Track) {
	defer func() {
		q.mu.Lock()
		q.isPlaying = false
		q.CurrentTrack = nil
		if q.stream != nil {
			q.stream.Close()
			q.stream = nil
		}
		q.mu.Unlock()

		log.Printf("[QUEUE] Track finished: %s in guild %s", track.Title, q.GuildID)
		// Play next automatically
		q.PlayNext()
	}()

	log.Printf("[QUEUE] Creating audio stream for: %s", track.Title)

	// Create the audio stream (this calls yt-dlp -> ffmpeg -> opus)
	stream, err := audio.NewStream(track.Query)
	if err != nil {
		log.Printf("[QUEUE] ERROR creating audio stream for '%s': %v", track.Title, err)
		return
	}

	q.mu.Lock()
	q.stream = stream
	q.mu.Unlock()

	// Get the voice connection - it may have dropped while we were waiting for yt-dlp
	conn := q.Bot.Client.VoiceManager.GetConn(q.GuildID)
	if conn == nil {
		log.Printf("[QUEUE] Voice connection lost while preparing stream. Attempting reconnect...")
		q.Bot.mu.RLock()
		channelID, exists := q.Bot.ActiveChannels[q.GuildID]
		q.Bot.mu.RUnlock()
		if exists {
			JoinVoiceChannel(q.Bot, q.GuildID, channelID)
			conn = q.Bot.Client.VoiceManager.GetConn(q.GuildID)
		}
		if conn == nil {
			log.Printf("[QUEUE] ERROR: Could not reconnect to voice for guild %s", q.GuildID)
			return
		}
	}

	// CRITICAL: Set speaking flag BEFORE sending audio frames
	// Discord will ignore audio packets if we haven't declared we're speaking
	if err := conn.SetSpeaking(context.Background(), voice.SpeakingFlagMicrophone); err != nil {
		log.Printf("[QUEUE] WARNING: Failed to set speaking flag: %v", err)
	}

	// Set the audio provider to our stream
	log.Printf("[QUEUE] Setting opus frame provider for: %s", track.Title)
	conn.SetOpusFrameProvider(stream)

	log.Printf("[QUEUE] Now playing: %s", track.Title)

	// Wait for the track to finish or be cancelled
	select {
	case <-ctx.Done():
		log.Printf("[QUEUE] Track skipped/stopped: %s", track.Title)
		return
	case err := <-stream.WaitDone():
		if err != nil && err != io.EOF {
			log.Printf("[QUEUE] Stream error for '%s': %v", track.Title, err)
		}
		return
	}
}

func (q *GuildQueue) Skip() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cancelPlay != nil {
		log.Printf("[QUEUE] Skipping current track in guild %s", q.GuildID)
		q.cancelPlay()
	}
}

func (q *GuildQueue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.Tracks = []Track{} // Clear queue
	q.isPaused = false
	if q.cancelPlay != nil {
		log.Printf("[QUEUE] Stopping playback and clearing queue in guild %s", q.GuildID)
		q.cancelPlay()
	}
}

func (q *GuildQueue) Pause() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.isPlaying || q.isPaused {
		return false
	}
	q.isPaused = true
	conn := q.Bot.Client.VoiceManager.GetConn(q.GuildID)
	if conn != nil {
		// Stop sending frames by setting a silence provider
		conn.SetOpusFrameProvider(&audio.SilenceProvider{})
	}
	return true
}

func (q *GuildQueue) Resume() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.isPlaying || !q.isPaused || q.stream == nil {
		return false
	}
	q.isPaused = false
	conn := q.Bot.Client.VoiceManager.GetConn(q.GuildID)
	if conn != nil {
		// Resume sending frames from the stream
		conn.SetOpusFrameProvider(q.stream)
	}
	return true
}

func (q *GuildQueue) GetTracks() []Track {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	// Create a copy to prevent data races
	tracksCopy := make([]Track, len(q.Tracks))
	copy(tracksCopy, q.Tracks)
	return tracksCopy
}
