package bot

import (
	"context"
	"io"
	"log"
	"sync"
	"time"

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
	log.Printf("[AFK-BOT] [QUEUE] Track added: %s (guild %s, queue length: %d)", track.Title, q.GuildID, len(q.Tracks))
}

func (q *GuildQueue) PlayNext() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.isPlaying {
		log.Printf("[AFK-BOT] [QUEUE] Already playing in guild %s, skipping PlayNext", q.GuildID)
		return
	}

	if len(q.Tracks) == 0 {
		log.Printf("[AFK-BOT] [QUEUE] No more tracks in guild %s, switching to silence", q.GuildID)
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

	log.Printf("[AFK-BOT] [QUEUE] Starting playback: %s in guild %s", track.Title, q.GuildID)
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

		log.Printf("[AFK-BOT] [QUEUE] Track finished: %s in guild %s", track.Title, q.GuildID)
		// Play next automatically
		q.PlayNext()
	}()

	log.Printf("[AFK-BOT] [QUEUE] Creating audio stream for: %s", track.Title)

	// Create the audio stream (this calls yt-dlp -> ffmpeg -> opus)
	stream, err := audio.NewStream(track.Query)
	if err != nil {
		log.Printf("[AFK-BOT] [QUEUE] ERROR creating audio stream for '%s': %v", track.Title, err)
		return
	}

	q.mu.Lock()
	q.stream = stream
	q.mu.Unlock()

	// Ensure we have a valid, ready voice connection before sending audio
	var conn voice.Conn
	for i := 0; i < 5; i++ {
		conn = q.Bot.Client.VoiceManager.GetConn(q.GuildID)
		if conn == nil {
			log.Printf("[AFK-BOT] [QUEUE] No voice connection found. Attempting reconnect...")
			q.Bot.mu.RLock()
			channelID, exists := q.Bot.ActiveChannels[q.GuildID]
			q.Bot.mu.RUnlock()
			if exists {
				// Fire reconnect in background so we don't block
				go JoinVoiceChannel(q.Bot, q.GuildID, channelID)
			}
		} else {
			// Try to set speaking. If it succeeds, the connection is healthy.
			err := conn.SetSpeaking(context.Background(), voice.SpeakingFlagMicrophone)
			if err == nil {
				break // Connection is healthy and ready!
			}
			log.Printf("[AFK-BOT] [QUEUE] Voice connection not ready yet (%v), waiting...", err)
		}
		
		time.Sleep(2 * time.Second)
	}

	// Final check before we inject the stream
	if conn == nil || conn.SetSpeaking(context.Background(), voice.SpeakingFlagMicrophone) != nil {
		log.Printf("[AFK-BOT] [QUEUE] ERROR: Could not get a ready voice connection for guild %s after retries", q.GuildID)
		return
	}

	// Set the audio provider to our stream
	log.Printf("[AFK-BOT] [QUEUE] Setting opus frame provider for: %s", track.Title)
	conn.SetOpusFrameProvider(stream)

	log.Printf("[AFK-BOT] [QUEUE] Now playing: %s", track.Title)

	// Wait for the track to finish or be cancelled
	select {
	case <-ctx.Done():
		log.Printf("[AFK-BOT] [QUEUE] Track skipped/stopped: %s", track.Title)
		return
	case err := <-stream.WaitDone():
		if err != nil && err != io.EOF {
			log.Printf("[AFK-BOT] [QUEUE] Stream error for '%s': %v", track.Title, err)
		}
		return
	}
}

func (q *GuildQueue) Skip() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cancelPlay != nil {
		log.Printf("[AFK-BOT] [QUEUE] Skipping current track in guild %s", q.GuildID)
		q.cancelPlay()
	}
}

func (q *GuildQueue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.Tracks = []Track{} // Clear queue
	q.isPaused = false
	if q.cancelPlay != nil {
		log.Printf("[AFK-BOT] [QUEUE] Stopping playback and clearing queue in guild %s", q.GuildID)
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
