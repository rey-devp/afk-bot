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
		Embeds: []discord.Embed{buildEmbed("🔍 Mencari Lagu...", fmt.Sprintf("Mencari `%s` di YouTube/SoundCloud", query), 0x3498db)},
	})

	// We run this in a goroutine because yt-dlp might take a few seconds
	go func() {
		log.Printf("[AFK-BOT] [PLAY] Searching for: %s", query)
		result, err := audio.Search(context.Background(), query)
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
			Title:       result.Title,
			Query:       result.Query,
			Duration:    result.Duration,
			Thumbnail:   result.Thumbnail,
			Uploader:    result.Uploader,
			RequestedBy: event.Message.Author.ID,
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

// handleSkipCommand skips the current track.
func (b *Bot) handleSkipCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}
	queue := b.GetQueue(*event.Message.GuildID)
	queue.Skip()
	_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
		Embeds: []discord.Embed{buildEmbed("⏭️ Dilewati", "Melompat ke lagu berikutnya...", 0x9b59b6)},
	})
}

// handleStopCommand stops playback and clears the queue.
func (b *Bot) handleStopCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}
	queue := b.GetQueue(*event.Message.GuildID)
	queue.Stop()
	_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
		Embeds: []discord.Embed{buildEmbed("⏹️ Berhenti", "Musik dihentikan dan antrean dibersihkan.", 0xe74c3c)},
	})
}

// handlePauseCommand pauses the current track.
func (b *Bot) handlePauseCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}
	queue := b.GetQueue(*event.Message.GuildID)
	if queue.Pause() {
		_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
			Embeds: []discord.Embed{buildEmbed("⏸️ Dijeda", "Musik sedang dijeda.", 0xf1c40f)},
		})
	} else {
		_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
			Embeds: []discord.Embed{buildEmbed("⚠️ Gagal", "Tidak ada lagu yang sedang diputar atau sudah dijeda.", 0xff0000)},
		})
	}
}

// handleResumeCommand resumes the paused track.
func (b *Bot) handleResumeCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}
	queue := b.GetQueue(*event.Message.GuildID)
	if queue.Resume() {
		_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
			Embeds: []discord.Embed{buildEmbed("▶️ Dilanjutkan", "Memutar kembali musik.", 0x2ecc71)},
		})
	} else {
		_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
			Embeds: []discord.Embed{buildEmbed("⚠️ Gagal", "Tidak ada lagu yang dijeda.", 0xff0000)},
		})
	}
}

// handleQueueCommand displays the current queue.
func (b *Bot) handleQueueCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}
	queue := b.GetQueue(*event.Message.GuildID)
	
	queue.mu.Lock()
	current := queue.CurrentTrack
	queue.mu.Unlock()
	
	tracks := queue.GetTracks()

	if current == nil && len(tracks) == 0 {
		_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
			Embeds: []discord.Embed{buildEmbed("📜 Antrean Kosong", "Tidak ada lagu di antrean saat ini.", 0x95a5a6)},
		})
		return
	}

	var descBuilder strings.Builder
	
	if current != nil {
		descBuilder.WriteString(fmt.Sprintf("**Sedang Diputar:**\n🎵 [%s](%s) | `%s`\n\n", current.Title, current.Query, current.Duration))
	}

	if len(tracks) > 0 {
		descBuilder.WriteString("**Selanjutnya:**\n")
		for i, t := range tracks {
			if i >= 10 {
				descBuilder.WriteString(fmt.Sprintf("\n*...dan %d lagu lainnya*", len(tracks)-10))
				break
			}
			descBuilder.WriteString(fmt.Sprintf("`%d.` %s | `%s`\n", i+1, t.Title, t.Duration))
		}
	}

	_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
		Embeds: []discord.Embed{buildEmbed("📜 Daftar Antrean", descBuilder.String(), 0x3498db)},
	})
}

// handleNpCommand shows the currently playing track.
func (b *Bot) handleNpCommand(event *events.MessageCreate) {
	if event.Message.GuildID == nil {
		return
	}
	queue := b.GetQueue(*event.Message.GuildID)
	
	queue.mu.Lock()
	current := queue.CurrentTrack
	queue.mu.Unlock()

	if current == nil {
		_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
			Embeds: []discord.Embed{buildEmbed("🔇 Tidak Ada Musik", "Tidak ada lagu yang sedang diputar saat ini.", 0x95a5a6)},
		})
		return
	}

	embed := discord.Embed{
		Title:       current.Title,
		Description: fmt.Sprintf("**Durasi:** %s\n**Channel:** %s", current.Duration, current.Uploader),
		Color:       0x2ecc71,
		Thumbnail:   &discord.EmbedResource{URL: current.Thumbnail},
		Author:      &discord.EmbedAuthor{Name: "🎵 Sedang Diputar"},
	}

	_, _ = b.Client.Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
		Embeds: []discord.Embed{embed},
	})
}
