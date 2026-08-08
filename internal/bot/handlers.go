package bot

import (
	"log"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// onReady is called when the bot successfully connects to Discord.
func (b *Bot) onReady(event *events.Ready) {
	log.Printf("[BOT] %s is ready and online!", event.User.Tag())

	if b.Config.VoiceChannelID != "" {
		voiceChannelID, err := snowflake.Parse(b.Config.VoiceChannelID)
		if err != nil {
			log.Printf("[BOT] Invalid VOICE_CHANNEL_ID: %v", err)
			return
		}
		JoinVoiceChannel(b, b.GuildID, voiceChannelID)
	} else {
		log.Println("[BOT] Waiting for '!join' command in text channels.")
	}
}

// onMessageCreate handles incoming messages and checks for bot commands.
func (b *Bot) onMessageCreate(event *events.MessageCreate) {
	// Ignore messages from bots
	if event.Message.Author.Bot {
		return
	}

	switch event.Message.Content {
	case "!join":
		b.handleJoinCommand(event)
	}
}

// handleJoinCommand makes the bot join the voice channel of the user who sent the command.
func (b *Bot) handleJoinCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}

	// Find the user's current voice state
	voiceState, ok := b.Client.Caches().VoiceState(*event.Message.GuildID, event.Message.Author.ID)
	if !ok || voiceState.ChannelID == nil {
		_, _ = b.Client.Rest().CreateMessage(event.ChannelID, discord.MessageCreate{
			Content: "⚠️ Kamu harus berada di dalam Voice Channel terlebih dahulu!",
		})
		return
	}

	JoinVoiceChannel(b, *event.Message.GuildID, *voiceState.ChannelID)

	_, _ = b.Client.Rest().CreateMessage(event.ChannelID, discord.MessageCreate{
		Content: "✅ Berhasil masuk ke Voice Channel!",
	})
}
