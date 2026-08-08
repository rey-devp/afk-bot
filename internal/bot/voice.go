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
func JoinVoiceChannel(b *Bot, guildID snowflake.ID, channelID snowflake.ID) {
	conn := b.Client.VoiceManager.CreateConn(guildID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// selfMute=false, selfDeaf=true
	if err := conn.Open(ctx, channelID, false, true); err != nil {
		log.Printf("[VOICE] Error joining voice channel %s: %v", channelID, err)
		return
	}

	log.Printf("[VOICE] Joined voice channel %s successfully", channelID)

	// Attach the silence provider so the bot keeps sending empty Opus frames
	conn.SetOpusFrameProvider(&audio.SilenceProvider{})

	if err := conn.SetSpeaking(context.Background(), voice.SpeakingFlagMicrophone); err != nil {
		log.Printf("[VOICE] Error setting speaking flag: %v", err)
	}
}
