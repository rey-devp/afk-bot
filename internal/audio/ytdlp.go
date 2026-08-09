package audio

import (
	"log"
	"os"
)

var (
	ytdlpCookiesPath   string
	ytdlpJSRuntimePath string
)

// InitYtDlpConfig stores the configuration used by yt-dlp argument builders.
func InitYtDlpConfig(cookiesPath, jsRuntime string) {
	ytdlpCookiesPath = cookiesPath
	ytdlpJSRuntimePath = jsRuntime

	if jsRuntime == "" {
		ytdlpJSRuntimePath = "deno" // Default to deno as requested by user
	}
}

// BuildYtDlpArgs constructs the arguments for yt-dlp including runtime and cookies.
func BuildYtDlpArgs(baseArgs []string) []string {
	args := append([]string(nil), baseArgs...)

	// Always add ejs remote component to solve challenges
	args = append(args, "--remote-components", "ejs:github")

	// Set JS runtime
	args = append(args, "--js-runtimes", ytdlpJSRuntimePath)

	// Add cookies if the path is set and the file exists
	if ytdlpCookiesPath != "" {
		if stat, err := os.Stat(ytdlpCookiesPath); err == nil && stat.Size() > 0 {
			args = append(args, "--cookies", ytdlpCookiesPath)
			log.Printf("[AFK-BOT] [AUDIO] Using cookies from: %s", ytdlpCookiesPath)
		} else {
			log.Printf("[AFK-BOT] [AUDIO] Warning: Cookie file '%s' not found or empty, running without cookies.", ytdlpCookiesPath)
		}
	}

	return args
}
