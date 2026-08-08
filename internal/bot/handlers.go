package bot

import (
	"context"
	"log"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
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

	switch event.Message.Content {
	case "!join":
		b.handleJoinCommand(event)
	case "!leave":
		b.handleLeaveCommand(event)
	case "!help":
		b.handleHelpCommand(event)
	}
}

// handleJoinCommand makes the bot join the voice channel of the user who sent the command.
func (b *Bot) handleJoinCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}

	// Find the user's current voice state
	voiceState, ok := b.Client.Caches.VoiceState(*event.Message.GuildID, event.Message.Author.ID)
	if !ok || voiceState.ChannelID == nil {
		_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
			Content: "⚠️ Kamu harus berada di dalam Voice Channel terlebih dahulu!",
		})
		return
	}

	// Update the configured voice channel so the bot remembers where it belongs
	b.mu.Lock()
	b.ActiveChannels[*event.Message.GuildID] = *voiceState.ChannelID
	b.mu.Unlock()
	
	JoinVoiceChannel(b, *event.Message.GuildID, *voiceState.ChannelID)

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
Saya adalah Bot yang tukang tidur, izinkan saya untuk tidur di voice kalian. Panggil aku dengan ketik **!join** biar aku bisa nemenin kamu sambil tidur, mau nge kick aku? cukup ketik **!leave** maka aku akan pergi dengan sakit hati. 💔

**🛠️ Daftar Perintah & Cara Kerja:**
> **!join**  : Pastikan kamu sudah berada di dalam Voice Channel. Ketik ini agar aku ikut masuk dan tertidur di sana selamanya (24/7).
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
