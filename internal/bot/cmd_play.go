package bot

import (
	"context"
	"fmt"
	"log"
	"strings"

	"bot-afk/internal/audio"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

// handlePlayCommand handles searching and queuing music.
func (b *Bot) handlePlayCommand(event *events.MessageCreate, content string) {
	if event.Message.GuildID == nil {
		return
	}

	query := strings.TrimSpace(strings.TrimPrefix(content, "!play"))
	if query == "" {
		_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
			Embeds: []discord.Embed{buildEmbed("⚠️ Error", "Masukkan nama lagu atau URL!\nContoh: `!play shape of you`", 0xff0000)},
		})
		return
	}

	// Tell the user we are searching
	msg, _ := b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
		Embeds: []discord.Embed{buildEmbed("🔍 Mencari Lagu...", fmt.Sprintf("Mencari `%s` di SoundCloud", query), 0x3498db)},
	})

	// We run this in a goroutine because yt-dlp might take a few seconds
	go func() {
		log.Printf("[AFK-BOT] [%s] [PLAY] Searching for: %s", event.Message.GuildID.String(), query)
		result, err := audio.Search(context.Background(), event.Message.GuildID.String(), query)
		if err != nil {
			log.Printf("[AFK-BOT] [PLAY] Search failed for '%s': %v", query, err)

			errMsg := "Gagal menemukan atau memutar lagu tersebut."
			if strings.Contains(err.Error(), "YOUTUBE_BLOCKED") {
				errMsg = "**YouTube memblokir bot dari link ini!**\nSaran: Gunakan **NAMA LAGU** saja (contoh: `!play sesi potret`) agar bot bisa memutarnya melalui jalur alternatif (SoundCloud)."
			}

			if msg != nil {
				_, _ = b.Client.Rest.UpdateMessage(msg.ChannelID, msg.ID, discord.MessageUpdate{
					Embeds: &[]discord.Embed{buildEmbed("❌ Pencarian Gagal", errMsg, 0xff0000)},
				})
			}
			return
		}

		log.Printf("[AFK-BOT] [PLAY] Found: %s (query: %s)", result.Title, result.Query)

		queue := b.GetQueue(*event.Message.GuildID)

		queue.mu.Lock()
		wasPlaying := queue.isPlaying
		queue.mu.Unlock()

		track := Track{
			Title:         result.Title,
			Query:         result.Query,
			Duration:      result.Duration,
			Thumbnail:     result.Thumbnail,
			Uploader:      result.Uploader,
			RequestedBy:   event.Message.Author.ID,
			TextChannelID: event.ChannelID,
		}
		
		queue.AddTrack(track)

		if msg != nil {
			embed := discord.Embed{
				Title:       track.Title,
				Color:       0x2ecc71, // Green for playing, Blue for queued
				Thumbnail:   &discord.EmbedResource{URL: track.Thumbnail},
			}
			
			if wasPlaying {
				embed.Author = &discord.EmbedAuthor{Name: "Ditambahkan ke Antrean 📝"}
				embed.Color = 0x3498db 
			} else {
				embed.Author = &discord.EmbedAuthor{Name: "Memutar Lagu 🎵"}
			}
			
			desc := fmt.Sprintf("**Durasi:** %s\n**Channel:** %s", track.Duration, track.Uploader)
			embed.Description = desc

			_, _ = b.Client.Rest.UpdateMessage(msg.ChannelID, msg.ID, discord.MessageUpdate{
				Embeds: &[]discord.Embed{embed},
			})
		}

		// Check if bot is in a voice channel
		b.mu.RLock()
		_, inVoice := b.ActiveChannels[*event.Message.GuildID]
		b.mu.RUnlock()

		if !inVoice {
			log.Printf("[AFK-BOT] [PLAY] Bot not in voice, auto-joining...")
			b.handleJoinCommand(event, "!join")
		}

		queue.PlayNext()
	}()
}
