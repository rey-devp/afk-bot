package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

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
		
		results, err := audio.SearchMany(context.Background(), event.Message.GuildID.String(), query, 10)
		if err != nil {
			log.Printf("[AFK-BOT] [PLAY] Search failed for '%s': %v", query, err)

			errMsg := "Gagal menemukan lagu tersebut."
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

		// If only 1 result (e.g. direct URL), play it immediately
		if len(results) == 1 {
			result := results[0]
			log.Printf("[AFK-BOT] [PLAY] Found single result: %s (query: %s)", result.Title, result.Query)
			b.queueTrack(event, msg, result)
			return
		}

		// Present interactive selection menu for multiple results
		var descBuilder strings.Builder
		descBuilder.WriteString("Ketik angka **1** hingga **")
		descBuilder.WriteString(fmt.Sprintf("%d", len(results)))
		descBuilder.WriteString("** untuk memilih lagu:\n\n")

		for i, res := range results {
			descBuilder.WriteString(fmt.Sprintf("**%d.** %s `(%s)`\n", i+1, res.Title, res.Duration))
		}
		
		descBuilder.WriteString("\n*Pencarian ini akan dibatalkan dalam 1 menit.*")

		b.mu.Lock()
		b.PendingSearches[event.ChannelID] = &PendingSearch{
			UserID:    event.Message.Author.ID,
			Results:   results,
			ExpiresAt: time.Now().Add(1 * time.Minute),
		}
		b.mu.Unlock()

		if msg != nil {
			_, _ = b.Client.Rest.UpdateMessage(msg.ChannelID, msg.ID, discord.MessageUpdate{
				Embeds: &[]discord.Embed{buildEmbed("🔍 Hasil Pencarian", descBuilder.String(), 0x3498db)},
			})
		}
	}()
}

// queueTrack handles appending the track to the queue and updating the message.
func (b *Bot) queueTrack(event *events.MessageCreate, msg *discord.Message, result audio.SearchResult) {
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
}
