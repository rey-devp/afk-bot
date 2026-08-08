package bot

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// onReady is called when the bot successfully connects to Discord.
func (b *Bot) onReady(event *events.Ready) {
	log.Printf("[BOT] %s is ready and online!", event.User.Tag())
	log.Println("[BOT] Waiting for '!join' command in text channels across all servers.")
}

// onMessageCreate handles incoming messages and checks for bot commands.
func (b *Bot) onMessageCreate(event *events.MessageCreate) {
	// Ignore messages from bots
	if event.Message.Author.Bot {
		return
	}

	content := strings.TrimSpace(event.Message.Content)

	if strings.HasPrefix(content, "!join") {
		b.handleJoinCommand(event, content)
	} else if content == "!leave" {
		b.handleLeaveCommand(event)
	} else if content == "!help" {
		b.handleHelpCommand(event)
	}
}

// handleJoinCommand makes the bot join the voice channel of the user who sent the command.
func (b *Bot) handleJoinCommand(event *events.MessageCreate, content string) {
	if event.Message.GuildID == nil {
		return
	}

	args := strings.TrimSpace(strings.TrimPrefix(content, "!join"))
	var targetChannelID snowflake.ID

	if args != "" {
		// User provided a voice channel name
		channels, err := b.Client.Rest.GetGuildChannels(*event.Message.GuildID)
		if err != nil {
			log.Printf("[BOT] Error getting channels: %v", err)
			_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
				Content: "⚠️ Terjadi kesalahan saat mencari Voice Channel.",
			})
			return
		}

		for _, ch := range channels {
			if ch.Type() == discord.ChannelTypeGuildVoice && strings.EqualFold(ch.Name(), args) {
				targetChannelID = ch.ID()
				break
			}
		}

		if targetChannelID == 0 {
			_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
				Content: "⚠️ Voice Channel dengan nama **" + args + "** tidak ditemukan di server ini!",
			})
			return
		}
	} else {
		// Default to user's current voice state
		voiceState, ok := b.Client.Caches.VoiceState(*event.Message.GuildID, event.Message.Author.ID)
		if !ok || voiceState.ChannelID == nil {
			_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
				Content: "⚠️ Kamu harus berada di dalam Voice Channel terlebih dahulu, ATAU ketik: `!join <nama voice>`",
			})
			return
		}
		targetChannelID = *voiceState.ChannelID
	}

	// Update the configured voice channel so the bot remembers where it belongs
	b.mu.Lock()
	b.ActiveChannels[*event.Message.GuildID] = targetChannelID
	b.mu.Unlock()
	
	JoinVoiceChannel(b, *event.Message.GuildID, targetChannelID)

	_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
		Content: "✅ Berhasil masuk ke Voice Channel!",
	})
}

// handleLeaveCommand makes the bot leave the voice channel of the server.
func (b *Bot) handleLeaveCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}

	b.mu.Lock()
	delete(b.ActiveChannels, *event.Message.GuildID)
	b.mu.Unlock()

	conn := b.Client.VoiceManager.GetConn(*event.Message.GuildID)
	if conn != nil {
		conn.Close(context.Background())
		b.Client.VoiceManager.RemoveConn(*event.Message.GuildID)
	}

	_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
		Content: "👋 Berhasil keluar dari Voice Channel!",
	})
}

// handleHelpCommand sends a help message to introduce the bot and list its commands.
func (b *Bot) handleHelpCommand(event *events.MessageCreate) {
	helpText := `**Halo!** 💤
Saya adalah Bot yang tukang tidur, izinkan saya untuk tidur di voice kalian.

**🛠️ Daftar Perintah & Cara Kerja:**
> **!join [nama voice]** : Panggil aku agar masuk ke Voice Channel! Kamu bisa menyebutkan namanya secara spesifik (contoh: *!join Mabar*). Jika tidak, aku akan otomatis masuk ke channel tempat kamu berada.
> **!leave** : Ketik ini jika kamu bosan dan ingin mengusirku dari Voice Channel di server ini.
> **!help**  : Menampilkan panduan tidurku (pesan ini).

*Catatan Rahasia: Aku punya kekuatan kebal AFK! Discord tidak akan bisa menendangku secara otomatis. Tapi kalau kalian nekat menendangku secara manual, aku akan langsung menerobos masuk lagi dalam 2 detik! (Kecuali kalian menggunakan !leave)* 😤`

	_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
		Content: helpText,
	})
}

// onVoiceStateUpdate handles voice state changes, especially to automatically rejoin if kicked.
func (b *Bot) onVoiceStateUpdate(event *events.GuildVoiceStateUpdate) {
	// Only care about our own voice state
	if event.VoiceState.UserID != b.Client.ApplicationID {
		return
	}

	// If the bot's ChannelID becomes nil, it means it was disconnected (kicked/left)
	if event.VoiceState.ChannelID == nil {
		log.Printf("[BOT] Disconnected from voice channel in guild %s. Checking auto-rejoin...", event.VoiceState.GuildID)

		b.mu.RLock()
		channelID, exists := b.ActiveChannels[event.VoiceState.GuildID]
		b.mu.RUnlock()

		if exists {
			log.Printf("[BOT] Auto-rejoining channel %s in guild %s...", channelID, event.VoiceState.GuildID)
			// Wait a brief moment before rejoining to avoid rate limits or state conflicts
			go func() {
				time.Sleep(2 * time.Second)
				JoinVoiceChannel(b, event.VoiceState.GuildID, channelID)
			}()
		}
	}
}
