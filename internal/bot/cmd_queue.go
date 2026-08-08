package bot

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

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
