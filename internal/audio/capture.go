package audio

import (
	"fmt"
)

type CaptureConfig struct {
	SampleRate    int
	Channels      int
	FrameMs       int
	CaptureDevice string
}

type Capture struct {
	sampleRate  int
	channels    int
	frameSamples int
	readFn      func() ([]int16, error)
	closeFn     func() error
}

func NewCapture(cfg CaptureConfig) (*Capture, error) {
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 48000
	}
	if cfg.Channels <= 0 {
		cfg.Channels = 1
	}
	if cfg.FrameMs <= 0 {
		cfg.FrameMs = 20
	}

	impl, err := newPlatformCapture(cfg)
	if err != nil {
		return nil, err
	}

	frameSamples := cfg.SampleRate * cfg.FrameMs / 1000 * cfg.Channels
	return &Capture{
		sampleRate:   cfg.SampleRate,
		channels:     cfg.Channels,
		frameSamples: frameSamples,
		readFn:       impl.read,
		closeFn:      impl.close,
	}, nil
}

func (c *Capture) ReadFrame() ([]int16, error) {
	if c.readFn == nil {
		return nil, fmt.Errorf("capture not initialized")
	}
	pcm, err := c.readFn()
	if err != nil {
		return nil, err
	}
	if len(pcm) < c.frameSamples {
		return nil, fmt.Errorf("short read: got %d samples, want %d", len(pcm), c.frameSamples)
	}
	return pcm[:c.frameSamples], nil
}

func (c *Capture) SampleRate() int  { return c.sampleRate }
func (c *Capture) Channels() int    { return c.channels }
func (c *Capture) FrameSamples() int { return c.frameSamples }

func (c *Capture) Close() error {
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}

type platformCapture interface {
	read() ([]int16, error)
	close() error
}
