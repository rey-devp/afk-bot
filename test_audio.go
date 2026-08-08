package main

import (
	"fmt"
	"io"
	"log"
	"os/exec"

	"github.com/jonas747/dca"
)

func main() {
	url := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	
	options := dca.StdEncodeOptions
	opts := *options
	opts.RawOutput = true
	opts.Bitrate = 96
	opts.Application = "audio"

	// Run yt-dlp to download and pipe to stdout
	ytCmd := exec.Command("yt-dlp", "--force-ipv4", "-f", "bestaudio", "-o", "-", url)
	stdout, err := ytCmd.StdoutPipe()
	if err != nil {
		log.Fatalf("StdoutPipe err: %v", err)
	}

	if err := ytCmd.Start(); err != nil {
		log.Fatalf("Start err: %v", err)
	}

	fmt.Println("yt-dlp started, starting dca.EncodeMem...")
	encodeSession, err := dca.EncodeMem(stdout, &opts)
	if err != nil {
		ytCmd.Process.Kill()
		log.Fatalf("EncodeMem err: %v", err)
	}

	fmt.Println("Reading Opus frames...")
	count := 0
	for {
		_, err := encodeSession.OpusFrame()
		if err != nil {
			if err == io.EOF {
				fmt.Printf("Finished reading %d frames\n", count)
				break
			}
			log.Fatalf("OpusFrame err: %v", err)
		}
		count++
		if count%500 == 0 {
			fmt.Printf("Read %d frames...\n", count)
		}
		if count >= 1000 {
			fmt.Println("Successfully read 1000 frames. Exiting.")
			break
		}
	}
	
	encodeSession.Cleanup()
	ytCmd.Process.Kill()
}
