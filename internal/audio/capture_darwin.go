//go:build darwin

package audio

import (
	"fmt"
	"strings"

	"github.com/gen2brain/malgo"
)

// macOS has no built-in speaker loopback API. Capture from a virtual loopback device
// such as BlackHole (https://existential.audio/blackhole/) with system output routed to it.
type darwinCapture struct {
	ctx            *malgo.AllocatedContext
	device         *malgo.Device
	sampleRate     int
	channels       int
	captureChannels int
	frameSamples   int
	frameCh        chan []int16
	errCh          chan error
}

func newPlatformCapture(cfg CaptureConfig) (platformCapture, error) {
	frameSamples := cfg.SampleRate * cfg.FrameMs / 1000 * cfg.Channels

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		_ = message
	})
	if err != nil {
		return nil, fmt.Errorf("init malgo context: %w", err)
	}

	deviceInfo, err := findDarwinCaptureDevice(ctx, cfg.CaptureDevice)
	if err != nil {
		ctx.Uninit()
		ctx.Free()
		return nil, err
	}

	captureChannels := cfg.Channels
	if captureChannels == 1 {
		captureChannels = 2
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = uint32(captureChannels)
	deviceConfig.SampleRate = uint32(cfg.SampleRate)
	deviceConfig.Capture.DeviceID = deviceInfo.ID.Pointer()

	frameCh := make(chan []int16, 16)
	errCh := make(chan error, 1)

	onRecv := func(outputBuffer, inputBuffer []byte, frameCount uint32) {
		samples := int(frameCount) * captureChannels
		if samples <= 0 {
			return
		}
		pcm := bytesToInt16LE(inputBuffer[:samples*2])
		if cfg.Channels == 1 && captureChannels == 2 {
			pcm = downmixStereoToMono(pcm)
		}
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
		return nil, fmt.Errorf("init capture device %q: %w", deviceInfo.Name(), err)
	}

	if err := device.Start(); err != nil {
		device.Uninit()
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("start capture device: %w", err)
	}

	return &darwinCapture{
		ctx:             ctx,
		device:          device,
		sampleRate:      cfg.SampleRate,
		channels:        cfg.Channels,
		captureChannels: captureChannels,
		frameSamples:    frameSamples,
		frameCh:         frameCh,
		errCh:           errCh,
	}, nil
}

func (c *darwinCapture) read() ([]int16, error) {
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

func (c *darwinCapture) close() error {
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

func findDarwinCaptureDevice(ctx *malgo.AllocatedContext, preferName string) (*malgo.DeviceInfo, error) {
	devices, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("list capture devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no capture devices found")
	}

	if preferName != "" {
		for i := range devices {
			if strings.EqualFold(devices[i].Name(), preferName) ||
				strings.Contains(strings.ToLower(devices[i].Name()), strings.ToLower(preferName)) {
				return &devices[i], nil
			}
		}
		return nil, fmt.Errorf("capture device %q not found; available: %s", preferName, deviceNames(devices))
	}

	keywords := []string{"blackhole", "loopback", "soundflower"}
	for _, kw := range keywords {
		for i := range devices {
			if strings.Contains(strings.ToLower(devices[i].Name()), kw) {
				return &devices[i], nil
			}
		}
	}

	return nil, fmt.Errorf(
		"no loopback capture device found (install BlackHole and route system audio to it). Available: %s",
		deviceNames(devices),
	)
}

func deviceNames(devices []malgo.DeviceInfo) string {
	names := make([]string, len(devices))
	for i := range devices {
		names[i] = devices[i].Name()
	}
	return strings.Join(names, ", ")
}

func downmixStereoToMono(stereo []int16) []int16 {
	if len(stereo) < 2 {
		return stereo
	}
	mono := make([]int16, len(stereo)/2)
	for i := 0; i < len(mono); i++ {
		l := int32(stereo[i*2])
		r := int32(stereo[i*2+1])
		mono[i] = int16((l + r) / 2)
	}
	return mono
}
