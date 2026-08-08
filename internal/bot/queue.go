package bot

import (
	"context"
	"io"
	"log"
	"sync"

	"bot-afk/internal/audio"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jonas747/dca"
)

type Track struct {
	Title       string
	URL         string
	RequestedBy snowflake.ID
}

type GuildQueue struct {
	GuildID       snowflake.ID
	Bot           *Bot
	Tracks        []Track
	CurrentTrack  *Track
	mu            sync.Mutex
	isPlaying     bool
	cancelPlay    context.CancelFunc
	encodeSession *dca.EncodeSession
}

func (q *GuildQueue) AddTrack(track Track) {
	q.mu.Lock()
	q.Tracks = append(q.Tracks, track)
	q.mu.Unlock()
}

func (q *GuildQueue) PlayNext() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.isPlaying {
		return
	}

	if len(q.Tracks) == 0 {
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

	go q.playRoutine(ctx, track)
}

func (q *GuildQueue) playRoutine(ctx context.Context, track Track) {
	defer func() {
		q.mu.Lock()
		q.isPlaying = false
		q.CurrentTrack = nil
		if q.encodeSession != nil {
			q.encodeSession.Cleanup()
			q.encodeSession = nil
		}
		q.mu.Unlock()
		// Play next automatically
		q.PlayNext()
	}()

	log.Printf("[QUEUE] Playing track: %s in guild %s", track.Title, q.GuildID)

	session, err := audio.NewOpusStream(track.URL)
	if err != nil {
		log.Printf("[QUEUE] Error creating audio stream: %v", err)
		return
	}

	q.mu.Lock()
	q.encodeSession = session.Session
	q.mu.Unlock()

	conn := q.Bot.Client.VoiceManager.GetConn(q.GuildID)
	if conn == nil {
		return
	}

	// Set the audio provider to our ffmpeg Opus stream
	conn.SetOpusFrameProvider(session)

	// Wait for the track to finish or be cancelled
	select {
	case <-ctx.Done():
		// Skipped or stopped
		return
	case err := <-session.Done:
		if err != nil && err != io.EOF {
			log.Printf("[QUEUE] Stream ended with error: %v", err)
		}
		return
	}
}

func (q *GuildQueue) Skip() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cancelPlay != nil {
		q.cancelPlay()
	}
}

func (q *GuildQueue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.Tracks = []Track{} // Clear queue
	if q.cancelPlay != nil {
		q.cancelPlay()
	}
}
