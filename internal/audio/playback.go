package audio

import (
	"fmt"
)

type PlaybackConfig struct {
	SampleRate int
	Channels   int
	BufferMs   int
}

type Playback struct {
	sampleRate int
	channels   int
	writeFn    func([]int16) error
	resetFn    func()
	closeFn    func() error
}

func NewPlayback(cfg PlaybackConfig) (*Playback, error) {
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 48000
	}
	if cfg.Channels <= 0 {
		cfg.Channels = 1
	}
	if cfg.BufferMs <= 0 {
		cfg.BufferMs = 100
	}

	impl, err := newPlatformPlayback(cfg)
	if err != nil {
		return nil, err
	}

	return &Playback{
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		writeFn:    impl.write,
		resetFn:    impl.reset,
		closeFn:    impl.close,
	}, nil
}

func (p *Playback) WritePCM(pcm []int16) error {
	if p.writeFn == nil {
		return fmt.Errorf("playback not initialized")
	}
	return p.writeFn(pcm)
}

func (p *Playback) Reset() {
	if p.resetFn != nil {
		p.resetFn()
	}
}

func (p *Playback) SampleRate() int { return p.sampleRate }
func (p *Playback) Channels() int   { return p.channels }

func (p *Playback) Close() error {
	if p.closeFn != nil {
		return p.closeFn()
	}
	return nil
}

type platformPlayback interface {
	write([]int16) error
	reset()
	close() error
}
