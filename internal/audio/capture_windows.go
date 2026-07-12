//go:build windows

package audio

import (
	"fmt"

	"github.com/gen2brain/malgo"
)

type windowsCapture struct {
	ctx          *malgo.AllocatedContext
	device       *malgo.Device
	sampleRate   int
	channels     int
	frameSamples int
	frameCh      chan []int16
	errCh        chan error
}

func newPlatformCapture(cfg CaptureConfig) (platformCapture, error) {
	frameSamples := cfg.SampleRate * cfg.FrameMs / 1000 * cfg.Channels

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		_ = message
	})
	if err != nil {
		return nil, fmt.Errorf("init malgo context: %w", err)
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Loopback)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = uint32(cfg.Channels)
	deviceConfig.SampleRate = uint32(cfg.SampleRate)
	deviceConfig.Alsa.NoMMap = 1

	frameCh := make(chan []int16, 8)
	errCh := make(chan error, 1)

	onRecv := func(outputBuffer, inputBuffer []byte, frameCount uint32) {
		samples := int(frameCount) * cfg.Channels
		if samples <= 0 {
			return
		}
		pcm := bytesToInt16LE(inputBuffer[:samples*2])
		select {
		case frameCh <- pcm:
		default:
		}
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onRecv,
	})
	if err != nil {
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("init loopback device: %w", err)
	}

	if err := device.Start(); err != nil {
		device.Uninit()
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("start loopback device: %w", err)
	}

	return &windowsCapture{
		ctx:          ctx,
		device:       device,
		sampleRate:   cfg.SampleRate,
		channels:     cfg.Channels,
		frameSamples: frameSamples,
		frameCh:      frameCh,
		errCh:        errCh,
	}, nil
}

func (c *windowsCapture) read() ([]int16, error) {
	accum := make([]int16, 0, c.frameSamples*2)
	for len(accum) < c.frameSamples {
		select {
		case err := <-c.errCh:
			return nil, err
		case chunk := <-c.frameCh:
			accum = append(accum, chunk...)
		}
	}
	return accum[:c.frameSamples], nil
}

func (c *windowsCapture) close() error {
	if c.device != nil {
		_ = c.device.Stop()
		c.device.Uninit()
	}
	if c.ctx != nil {
		c.ctx.Uninit()
		c.ctx.Free()
	}
	return nil
}

