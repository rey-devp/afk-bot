package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"bot-afk/internal/bot"
	"bot-afk/internal/config"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	// Load configuration from environment / .env file
	cfg := config.Load()

	// Initialize and start the Discord bot
	b := bot.New(cfg)
	b.Start()

	// Start Fiber HTTP server for health checks
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Use(logger.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	go func() {
		log.Printf("[HTTP] Starting Fiber server on port %s", cfg.Port)
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Fatalf("[HTTP] Server error: %v", err)
		}
	}()

	log.Println("[APP] Bot is now running. Press CTRL-C to exit.")

	// Wait for termination signal
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Graceful shutdown
	b.Stop()
	_ = app.Shutdown()
}
