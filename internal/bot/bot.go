package bot

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"bot-afk/internal/audio"
	"bot-afk/internal/config"

	"github.com/disgoorg/disgo"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave/golibdave"
	"github.com/disgoorg/snowflake/v2"
)

// Bot holds the DisGo client, configuration, and multi-guild state.
type Bot struct {
	Client         *disgobot.Client
	Config         *config.Config
	ActiveChannels map[snowflake.ID]snowflake.ID
	Queues         map[snowflake.ID]*GuildQueue
	mu             sync.RWMutex
}

// GetQueue returns the music queue for the given guild.
func (b *Bot) GetQueue(guildID snowflake.ID) *GuildQueue {
	b.mu.Lock()
	defer b.mu.Unlock()

	q, exists := b.Queues[guildID]
	if !exists {
		q = &GuildQueue{
			GuildID: guildID,
			Bot:     b,
			Tracks:  []Track{},
		}
		b.Queues[guildID] = q
	}
	return q
}

// New creates and configures a new Bot instance.
func New(cfg *config.Config) *Bot {
	b := &Bot{
		Config:         cfg,
		ActiveChannels: make(map[snowflake.ID]snowflake.ID),
		Queues:         make(map[snowflake.ID]*GuildQueue),
	}

	// Initialize yt-dlp configuration for audio module
	audio.InitYtDlpConfig(cfg.YtdlpCookiesPath, cfg.YtdlpJSRuntimePath)

	client, err := disgo.New(cfg.BotToken,
		disgobot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuildVoiceStates,
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent,
			),
		),
		disgobot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagVoiceStates),
		),
		disgobot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
		),
	)
	if err != nil {
		log.Fatalf("[BOT] Error creating DisGo client: %v", err)
	}

	// Register event listeners
	client.AddEventListeners(
		disgobot.NewListenerFunc(b.onReady),
		disgobot.NewListenerFunc(b.onMessageCreate),
		disgobot.NewListenerFunc(b.onVoiceStateUpdate),
	)

	b.Client = client
	return b
}

// Start opens the gateway connection to Discord.
func (b *Bot) Start() {
	b.checkYtDlpVersion()

	if err := b.Client.OpenGateway(context.TODO()); err != nil {
		log.Fatalf("[BOT] Error opening gateway: %v", err)
	}
	log.Println("[BOT] Gateway connection opened.")
}

// checkYtDlpVersion verifies if the installed yt-dlp is older than 30 days
func (b *Bot) checkYtDlpVersion() {
	out, err := exec.Command("yt-dlp", "--version").Output()
	if err != nil {
		log.Printf("[WARNING] Could not check yt-dlp version: %v", err)
		return
	}
	
	versionStr := strings.TrimSpace(string(out))
	// yt-dlp version format is typically YYYY.MM.DD (e.g., 2024.08.06)
	parsedDate, err := time.Parse("2006.01.02", versionStr)
	if err == nil {
		if time.Since(parsedDate).Hours() > 24 * 30 {
			log.Printf("[WARNING] yt-dlp version (%s) is older than 30 days! Please update using 'yt-dlp -U' to prevent YouTube extraction errors.", versionStr)
		} else {
			log.Printf("[BOT] yt-dlp version %s is up-to-date.", versionStr)
		}
	} else {
		log.Printf("[BOT] yt-dlp version string '%s' could not be parsed as date, skipping check.", versionStr)
	}
}

// Stop gracefully closes the Discord connection.
func (b *Bot) Stop() {
	log.Println("[BOT] Shutting down...")
	b.Client.Close(context.TODO())
}
