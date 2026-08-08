package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration values loaded from environment variables.
type Config struct {
	BotToken       string
	GuildID        string
	VoiceChannelID string
	Port           string
}

// Load reads environment variables (and .env file if present) and returns a Config.
func Load() *Config {
	// Silently load .env file if it exists
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	cfg := &Config{
		BotToken:       os.Getenv("BOT_TOKEN"),
		GuildID:        os.Getenv("GUILD_ID"),
		VoiceChannelID: os.Getenv("VOICE_CHANNEL_ID"),
		Port:           port,
	}

	// Validate required fields
	if cfg.BotToken == "" {
		log.Fatal("[CONFIG] BOT_TOKEN environment variable is not set")
	}
	if cfg.GuildID == "" {
		log.Fatal("[CONFIG] GUILD_ID environment variable is not set")
	}

	return cfg
}
