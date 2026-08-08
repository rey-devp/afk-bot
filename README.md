# 🤖 Discord 24/7 AFK Bot

A lightweight Go bot that joins a Discord Voice Channel and stays there 24/7 by broadcasting silent Opus frames. Built with [DisGo](https://github.com/disgoorg/disgo) and supports Discord's DAVE (E2EE) protocol.

## Features

- **Auto-Join** — Automatically joins a voice channel on startup (if `VOICE_CHANNEL_ID` is set).
- **`!join` Command** — Join the user's current voice channel via text command.
- **Anti-Disconnect** — Continuously sends Opus silence frames to avoid idle timeout.
- **Self-Deafen** — Bot deafens itself to save bandwidth.
- **Health Check** — Built-in [Fiber](https://gofiber.io/) HTTP server for hosting platforms like Railway.
- **DAVE Protocol** — Full E2EE/DAVE encryption support via `godave`.

## Project Structure

```
afk-bot/
├── cmd/bot/main.go              # Application entry point
├── internal/
│   ├── audio/silence.go         # Opus silence frame provider
│   ├── bot/
│   │   ├── bot.go               # Bot initialization & lifecycle
│   │   ├── handlers.go          # Discord event handlers
│   │   └── voice.go             # Voice channel join logic
│   └── config/config.go         # Environment variable loader
├── .env.example                 # Example environment variables
├── .gitignore
├── Dockerfile                   # Multi-stage Docker build (CGO)
├── go.mod
└── go.sum
```

## Environment Variables

| Variable           | Required | Description                                        |
| ------------------ | -------- | -------------------------------------------------- |
| `BOT_TOKEN`        | ✅       | Discord bot token                                  |
| `GUILD_ID`         | ✅       | Discord server (guild) ID                          |
| `VOICE_CHANNEL_ID` | ❌       | Voice channel ID for auto-join on startup          |
| `PORT`             | ❌       | HTTP server port (default: `8080`)                 |

## Deploy to Railway

1. Push this repository to GitHub.
2. Create a new project on [Railway](https://railway.app/) → **Deploy from GitHub repo**.
3. Add the environment variables (`BOT_TOKEN`, `GUILD_ID`) in the Railway dashboard.
4. Railway will automatically detect the `Dockerfile` and deploy.

## License

See [LICENSE](LICENSE) file.
