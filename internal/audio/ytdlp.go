package audio

import (
	"log"
	"os"
	"os/exec"
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

	// 1. Startup check for Cookies
	if ytdlpCookiesPath != "" {
		if _, err := os.Stat(ytdlpCookiesPath); err != nil {
			log.Printf("[AFK-BOT] [STARTUP-WARNING] YTDLP_COOKIES_PATH is set to '%s' but file not found: %v", ytdlpCookiesPath, err)
		} else {
			log.Printf("[AFK-BOT] [STARTUP-INFO] Valid cookies file detected at: %s", ytdlpCookiesPath)
		}
	} else {
		log.Printf("[AFK-BOT] [STARTUP-WARNING] YTDLP_COOKIES_PATH is not set, yt-dlp will run without cookies")
	}

	// 2. Startup check for JS Runtime
	runtimeBinary := ytdlpJSRuntimePath
	// If the path contains a colon (e.g. deno:/path), split it to check the binary name or path
	// But usually it's just 'deno' or 'node'
	if path, err := exec.LookPath(runtimeBinary); err != nil {
		log.Printf("[AFK-BOT] [STARTUP-WARNING] JS Runtime '%s' not found in system PATH. yt-dlp might fail to solve challenges!", runtimeBinary)
	} else {
		log.Printf("[AFK-BOT] [STARTUP-INFO] JS Runtime '%s' detected at: %s", runtimeBinary, path)
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


	log.Printf("[AFK-BOT] [AUDIO] Running yt-dlp with args: %v", args)
	return args
}
