package main

// SilenceProvider implements voice.OpusFrameProvider
type SilenceProvider struct{}

// ProvideOpusFrame returns a standard 20ms Opus silence frame (3 bytes)
func (p *SilenceProvider) ProvideOpusFrame() ([]byte, error) {
	return []byte{0xF8, 0xFF, 0xFE}, nil
}

// Close is a no-op for silence provider
func (p *SilenceProvider) Close() {}
