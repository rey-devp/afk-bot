package bot

import (
	"context"
	"log"

	"bot-afk/internal/config"

	"github.com/disgoorg/disgo"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave/golibdave"
	"github.com/disgoorg/snowflake/v2"
)

// Bot holds the DisGo client, configuration, and parsed identifiers.
type Bot struct {
	Client  disgobot.Client
	Config  *config.Config
	GuildID snowflake.ID
}

// New creates and configures a new Bot instance.
func New(cfg *config.Config) *Bot {
	guildID, err := snowflake.Parse(cfg.GuildID)
	if err != nil {
		log.Fatalf("[BOT] Invalid GUILD_ID: %v", err)
	}

	b := &Bot{
		Config:  cfg,
		GuildID: guildID,
	}

	client, err := disgo.New("Bot "+cfg.BotToken,
		disgobot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuildVoiceStates,
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent,
			),
		),
		disgobot.WithVoiceConfigOpts(
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
	)

	b.Client = client
	return b
}

// Start opens the gateway connection to Discord.
func (b *Bot) Start() {
	if err := b.Client.OpenGateway(context.TODO()); err != nil {
		log.Fatalf("[BOT] Error opening gateway: %v", err)
	}
	log.Println("[BOT] Gateway connection opened.")
}

// Stop gracefully closes the Discord connection.
func (b *Bot) Stop() {
	log.Println("[BOT] Shutting down...")
	b.Client.Close(context.TODO())
}
