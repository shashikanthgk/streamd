package codec

import (
	"fmt"

	"gopkg.in/hraban/opus.v2"
)

const (
	DefaultSampleRate = 48000
	DefaultChannels   = 1
	DefaultFrameMs    = 20
)

type Encoder struct {
	enc        *opus.Encoder
	sampleRate int
	channels   int
	frameMs    int
}

type Decoder struct {
	dec        *opus.Decoder
	sampleRate int
	channels   int
	frameMs    int
}

func NewEncoder(sampleRate, channels, frameMs int) (*Encoder, error) {
	if sampleRate <= 0 {
		sampleRate = DefaultSampleRate
	}
	if channels <= 0 {
		channels = DefaultChannels
	}
	if frameMs <= 0 {
		frameMs = DefaultFrameMs
	}

	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return nil, fmt.Errorf("create opus encoder: %w", err)
	}

	if err := enc.SetBitrate(64000); err != nil {
		return nil, fmt.Errorf("set opus bitrate: %w", err)
	}
	if err := enc.SetDTX(false); err != nil {
		return nil, fmt.Errorf("set opus dtx: %w", err)
	}

	return &Encoder{
		enc:        enc,
		sampleRate: sampleRate,
		channels:   channels,
		frameMs:    frameMs,
	}, nil
}

func (e *Encoder) Encode(pcm []int16) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("empty pcm buffer")
	}
	out := make([]byte, 4000)
	n, err := e.enc.Encode(pcm, out)
	if err != nil {
		return nil, fmt.Errorf("encode opus frame: %w", err)
	}
	return out[:n], nil
}

func (e *Encoder) SampleRate() int { return e.sampleRate }
func (e *Encoder) Channels() int   { return e.channels }
func (e *Encoder) FrameMs() int    { return e.frameMs }

func NewDecoder(sampleRate, channels, frameMs int) (*Decoder, error) {
	if sampleRate <= 0 {
		sampleRate = DefaultSampleRate
	}
	if channels <= 0 {
		channels = DefaultChannels
	}
	if frameMs <= 0 {
		frameMs = DefaultFrameMs
	}

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("create opus decoder: %w", err)
	}

	return &Decoder{
		dec:        dec,
		sampleRate: sampleRate,
		channels:   channels,
		frameMs:    frameMs,
	}, nil
}

func (d *Decoder) Decode(opusFrame []byte) ([]int16, error) {
	if len(opusFrame) == 0 {
		return nil, fmt.Errorf("empty opus frame")
	}
	pcm := make([]int16, d.sampleRate*d.frameMs/1000*d.channels)
	n, err := d.dec.Decode(opusFrame, pcm)
	if err != nil {
		return nil, fmt.Errorf("decode opus frame: %w", err)
	}
	return pcm[:n], nil
}

func (d *Decoder) SampleRate() int { return d.sampleRate }
func (d *Decoder) Channels() int   { return d.channels }
func (d *Decoder) FrameMs() int    { return d.frameMs }
