package audio

import (
	"encoding/base64"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
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

			// Process the file to handle base64 encoding and read-only file systems (like Render secrets)
			if content, err := os.ReadFile(ytdlpCookiesPath); err == nil && len(content) > 0 {
				strContent := strings.TrimSpace(string(content))
				
				// Try to decode as base64 in case user provided a base64 encoded string
				if decoded, err := base64.StdEncoding.DecodeString(strContent); err == nil && strings.Contains(string(decoded), "# Netscape") {
					content = decoded
					log.Printf("[AFK-BOT] [STARTUP-INFO] Decoded base64 cookies file.")
				}
				
				// Write to a temporary file in /tmp so yt-dlp can update the file without read-only errors
				tmpFile := "/tmp/ytdlp_cookies.txt"
				if err := os.WriteFile(tmpFile, content, 0644); err != nil {
					log.Printf("[AFK-BOT] [STARTUP-WARNING] Failed to write cookies to temp file: %v", err)
				} else {
					log.Printf("[AFK-BOT] [STARTUP-INFO] Copied cookies to temp file: %s", tmpFile)
					ytdlpCookiesPath = tmpFile // Override path to the writable temp file
				}
			}
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

	// 3. Startup check for PO Token Provider server
	go checkPOTokenProvider()

	// 4. Startup check for yt-dlp PO Token Plugin
	go checkPOTokenPlugin()
}

func checkPOTokenProvider() {
	const maxAttempts = 10
	const retryDelay = 1 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:4416", 2*time.Second)
		if err == nil {
			conn.Close()
			log.Printf("[AFK-BOT] [STARTUP-INFO] PO Token provider reachable at 127.0.0.1:4416 (attempt %d/%d)", attempt, maxAttempts)
			return
		}

		if attempt < maxAttempts {
			log.Printf("[AFK-BOT] [STARTUP-INFO] PO Token provider not ready yet (attempt %d/%d), retrying in %v...", attempt, maxAttempts, retryDelay)
			time.Sleep(retryDelay)
		} else {
			log.Printf("[AFK-BOT] [STARTUP-WARNING] PO Token provider still not reachable after %d attempts: %v", maxAttempts, err)
		}
	}
}

func checkPOTokenPlugin() {
	cmd := exec.Command("yt-dlp", "-v", "--simulate", "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	output, _ := cmd.CombinedOutput()
	outputStr := string(output)

	if strings.Contains(outputStr, "PO Token Providers") {
		// Cari baris yang mengandung info ini untuk di-log
		for _, line := range strings.Split(outputStr, "\n") {
			if strings.Contains(line, "PO Token Providers") {
				log.Printf("[AFK-BOT] [STARTUP-INFO] %s", strings.TrimSpace(line))
			}
		}
	} else {
		log.Printf("[AFK-BOT] [STARTUP-WARNING] yt-dlp verbose output does NOT mention PO Token Providers — plugin may not be installed or not detected")
	}
}

// BuildYtDlpArgs constructs the arguments for yt-dlp including runtime and cookies.
func BuildYtDlpArgs(guildID string, baseArgs []string, playerClient string) []string {
	args := append([]string(nil), baseArgs...)

	// Always add ejs remote component to solve challenges
	args = append(args, "--remote-components", "ejs:github")

	// Use specific player client if provided (to bypass YouTube web-based bot detection)
	if playerClient != "" {
		args = append(args, "--extractor-args", "youtube:player_client="+playerClient)
	}

	// Set JS runtime
	args = append(args, "--js-runtimes", ytdlpJSRuntimePath)

	// Add cookies if the path is set and the file exists
	if ytdlpCookiesPath != "" {
		if stat, err := os.Stat(ytdlpCookiesPath); err == nil && stat.Size() > 0 {
			args = append(args, "--cookies", ytdlpCookiesPath)
			log.Printf("[AFK-BOT] [%s] [AUDIO] Using cookies from: %s", guildID, ytdlpCookiesPath)
		} else {
			log.Printf("[AFK-BOT] [%s] [AUDIO] Warning: Cookie file '%s' not found or empty, running without cookies.", guildID, ytdlpCookiesPath)
		}
	}


	log.Printf("[AFK-BOT] [%s] [AUDIO] Running yt-dlp with args: %v", guildID, args)
	return args
}
