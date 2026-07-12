//go:build windows || linux || darwin

package audio

import (
	"fmt"
	"sync"

	"github.com/gen2brain/malgo"
)

const maxPlaybackBufferSamples = 48000 / 5 // ~200ms at 48kHz mono

type malgoPlayback struct {
	ctx        *malgo.AllocatedContext
	device     *malgo.Device
	sampleRate int
	channels   int
	mu         sync.Mutex
	buffer     []int16
	notify     chan struct{}
	closed     bool
}

func newPlatformPlayback(cfg PlaybackConfig) (platformPlayback, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		_ = message
	})
	if err != nil {
		return nil, fmt.Errorf("init malgo context: %w", err)
	}

	mp := &malgoPlayback{
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		buffer:     make([]int16, 0, cfg.SampleRate*cfg.Channels),
		notify:     make(chan struct{}, 1),
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = uint32(cfg.Channels)
	deviceConfig.SampleRate = uint32(cfg.SampleRate)
	deviceConfig.PeriodSizeInFrames = uint32(cfg.SampleRate * cfg.BufferMs / 1000)

	onSend := func(outputBuffer, inputBuffer []byte, frameCount uint32) {
		samplesNeeded := int(frameCount) * cfg.Channels
		mp.mu.Lock()
		defer mp.mu.Unlock()

		if len(mp.buffer) >= samplesNeeded {
			copy(outputBuffer, int16ToBytesLE(mp.buffer[:samplesNeeded]))
			mp.buffer = mp.buffer[samplesNeeded:]
		} else {
			for i := range outputBuffer {
				outputBuffer[i] = 0
			}
		}
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onSend,
	})
	if err != nil {
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("init playback device: %w", err)
	}

	if err := device.Start(); err != nil {
		device.Uninit()
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("start playback device: %w", err)
	}

	mp.ctx = ctx
	mp.device = device
	return mp, nil
}

func (p *malgoPlayback) write(pcm []int16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("playback closed")
	}
	p.buffer = append(p.buffer, pcm...)
	if len(p.buffer) > maxPlaybackBufferSamples {
		p.buffer = append([]int16(nil), p.buffer[len(p.buffer)-maxPlaybackBufferSamples:]...)
	}
	return nil
}

func (p *malgoPlayback) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buffer = p.buffer[:0]
}

func (p *malgoPlayback) close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.device != nil {
		_ = p.device.Stop()
		p.device.Uninit()
		p.device = nil
	}
	if p.ctx != nil {
		p.ctx.Uninit()
		p.ctx.Free()
		p.ctx = nil
	}
	return nil
}
