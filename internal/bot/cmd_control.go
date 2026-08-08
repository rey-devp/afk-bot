package bot

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

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
