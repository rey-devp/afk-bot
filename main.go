package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave/golibdave"
	"github.com/disgoorg/snowflake/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"
)

var (
	Token             string
	GuildIDStr        string
	VoiceChannelIDStr string
	Port              string
)

func init() {
	_ = godotenv.Load()

	Token = os.Getenv("BOT_TOKEN")
	GuildIDStr = os.Getenv("GUILD_ID")
	VoiceChannelIDStr = os.Getenv("VOICE_CHANNEL_ID")
	Port = os.Getenv("PORT")
	if Port == "" {
		Port = "8080"
	}
}

func main() {
	if Token == "" {
		log.Fatal("BOT_TOKEN environment variable is not set")
	}
	if GuildIDStr == "" {
		log.Fatal("GUILD_ID environment variable is not set")
	}

	GuildID, err := snowflake.Parse(GuildIDStr)
	if err != nil {
		log.Fatalf("Invalid GUILD_ID: %v", err)
	}

	// Initialize DisGo client
	client, err := disgo.New("Bot "+Token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuildVoiceStates,
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent,
			),
		),
		// Configure voice with DAVE session provider
		bot.WithVoiceConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
		),
	)
	if err != nil {
		log.Fatalf("Error creating DisGo client: %v", err)
	}

	client.AddEventListeners(bot.NewListenerFunc(func(event *events.Ready) {
		log.Printf("Bot %s is ready and online!", event.User.Tag())

		if VoiceChannelIDStr != "" {
			voiceChannelID, err := snowflake.Parse(VoiceChannelIDStr)
			if err != nil {
				log.Printf("Invalid VOICE_CHANNEL_ID: %v", err)
				return
			}
			joinVoiceChannel(client, GuildID, voiceChannelID)
		} else {
			log.Println("Bot is ready and waiting for '!join' command in text channels.")
		}
	}))

	client.AddEventListeners(bot.NewListenerFunc(func(event *events.MessageCreate) {
		if event.Message.Author.Bot {
			return
		}

		if event.Message.Content == "!join" {
			if event.Message.GuildID == nil {
				return
			}

			// Find user's voice state
			voiceState, ok := client.Caches().VoiceState(*event.Message.GuildID, event.Message.Author.ID)
			if !ok || voiceState.ChannelID == nil {
				_, _ = client.Rest().CreateMessage(event.ChannelID, discord.MessageCreate{
					Content: "Kamu harus berada di dalam Voice Channel terlebih dahulu!",
				})
				return
			}

			joinVoiceChannel(client, *event.Message.GuildID, *voiceState.ChannelID)
			_, _ = client.Rest().CreateMessage(event.ChannelID, discord.MessageCreate{
				Content: "Berhasil masuk ke Voice Channel!",
			})
		}
	}))

	// Open gateway connection
	if err = client.OpenGateway(context.TODO()); err != nil {
		log.Fatalf("Error opening gateway connection: %v", err)
	}

	// Start Fiber HTTP server
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Use(logger.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	go func() {
		log.Printf("Starting Fiber HTTP server on port %s", Port)
		if err := app.Listen(":" + Port); err != nil {
			log.Fatalf("Fiber server error: %v", err)
		}
	}()

	log.Println("Bot is now running. Press CTRL-C to exit.")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s

	log.Println("Shutting down...")
	client.Close(context.TODO())
}

func joinVoiceChannel(client bot.Client, guildID snowflake.ID, channelID snowflake.ID) {
	conn := client.VoiceManager().CreateConn(guildID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := conn.Open(ctx, channelID, false, true) // selfMute=false, selfDeaf=true
	if err != nil {
		log.Printf("Error joining voice channel: %v", err)
		return
	}

	log.Printf("Joined voice channel %s successfully", channelID)

	conn.SetOpusFrameProvider(&SilenceProvider{})

	if err := conn.SetSpeaking(context.Background(), voice.SpeakingFlagMicrophone); err != nil {
		log.Printf("Error setting speaking: %v", err)
	}
}
