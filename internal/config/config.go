package config

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	BotToken           string
	Port               string
	YtdlpCookiesPath   string
	YtdlpJSRuntimePath string
}

// Load reads environment variables (and .env file if present) and returns a Config.
func Load() *Config {
	// Silently load .env file if it exists
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	botToken := os.Getenv("BOT_TOKEN")
	botToken = strings.TrimSpace(botToken)
	botToken = strings.Trim(botToken, "\"") // Remove quotes if any
	botToken = strings.TrimPrefix(botToken, "Bot ") // Remove Bot prefix if any

	cfg := &Config{
		BotToken:           botToken,
		Port:               port,
		YtdlpCookiesPath:   os.Getenv("YTDLP_COOKIES_PATH"),
		YtdlpJSRuntimePath: os.Getenv("YTDLP_JS_RUNTIME_PATH"),
	}

	if cfg.BotToken == "" {
		log.Fatal("[CONFIG] BOT_TOKEN environment variable is not set")
	}

	return cfg
}
