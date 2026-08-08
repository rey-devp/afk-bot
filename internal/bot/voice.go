package bot

import (
	"context"
	"log"
	"time"

	"bot-afk/internal/audio"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

// JoinVoiceChannel connects the bot to the specified voice channel
// in a deafened state and starts broadcasting silence frames.
// It runs in a goroutine and retries up to 3 times if it fails (useful for Render network drops).
func JoinVoiceChannel(b *Bot, guildID snowflake.ID, channelID snowflake.ID) {
	go func() {
		conn := b.Client.VoiceManager.CreateConn(guildID)

		maxRetries := 3
		for i := 1; i <= maxRetries; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			
			log.Printf("[AFK-BOT] [VOICE] Attempt %d/%d to join voice channel %s...", i, maxRetries, channelID)
			err := conn.Open(ctx, channelID, false, true)
			cancel()

			if err == nil {
				log.Printf("[AFK-BOT] [VOICE] Joined voice channel %s successfully", channelID)
				
				// Attach the silence provider so the bot keeps sending empty Opus frames
				conn.SetOpusFrameProvider(&audio.SilenceProvider{})

				if err := conn.SetSpeaking(context.Background(), voice.SpeakingFlagMicrophone); err != nil {
					log.Printf("[AFK-BOT] [VOICE] Error setting speaking flag: %v", err)
				}
				return
			}

			log.Printf("[AFK-BOT] [VOICE] Failed attempt %d to join %s: %v", i, channelID, err)
			
			if i < maxRetries {
				time.Sleep(3 * time.Second) // wait before retry
			}
		}

		log.Printf("[AFK-BOT] [VOICE] ERROR: Exhausted all retries. Could not join voice channel %s", channelID)
	}()
}
