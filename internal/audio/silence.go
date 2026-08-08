package audio

// SilenceProvider implements voice.OpusFrameProvider.
// It continuously returns a standard 20ms Opus silence frame (3 bytes)
// to prevent Discord from disconnecting the bot due to inactivity.
type SilenceProvider struct{}

// ProvideOpusFrame returns a single Opus silence frame.
func (p *SilenceProvider) ProvideOpusFrame() ([]byte, error) {
	return []byte{0xF8, 0xFF, 0xFE}, nil
}

// Close is a no-op for the silence provider.
func (p *SilenceProvider) Close() {}
