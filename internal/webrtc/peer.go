package webrtc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/streamd/streamd/internal/audio"
	"github.com/streamd/streamd/internal/codec"
	"github.com/streamd/streamd/internal/config"
	"github.com/streamd/streamd/internal/signaling"
)

type PeerConfig struct {
	STUNServers []string
	Audio       config.AudioConfig
}

type SenderSession struct {
	pc       *webrtc.PeerConnection
	track    *webrtc.TrackLocalStaticSample
	capture  *audio.Capture
	encoder  *codec.Encoder
	logger   *slog.Logger
	stopOnce sync.Once
	stopCh   chan struct{}
}

type ReceiverSession struct {
	pc       *webrtc.PeerConnection
	playback *audio.Playback
	decoder  *codec.Decoder
	logger   *slog.Logger
	stopOnce sync.Once
	stopCh   chan struct{}
}

func NewPeerConnection(cfg PeerConfig) (*webrtc.PeerConnection, error) {
	iceServers := make([]webrtc.ICEServer, 0, len(cfg.STUNServers))
	for _, stun := range cfg.STUNServers {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: []string{stun}})
	}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("register codecs: %w", err)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		return nil, fmt.Errorf("create peer connection: %w", err)
	}

	return pc, nil
}

func NewSenderSession(cfg PeerConfig, logger *slog.Logger) (*SenderSession, error) {
	if logger == nil {
		logger = slog.Default()
	}

	pc, err := NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"streamd",
	)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("create audio track: %w", err)
	}

	if _, err := pc.AddTrack(track); err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("add audio track: %w", err)
	}

	capture, err := audio.NewCapture(audio.CaptureConfig{
		SampleRate:    cfg.Audio.SampleRate,
		Channels:      cfg.Audio.Channels,
		FrameMs:       cfg.Audio.FrameMs,
		CaptureDevice: cfg.Audio.CaptureDevice,
	})
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("init audio capture: %w", err)
	}

	encoder, err := codec.NewEncoder(cfg.Audio.SampleRate, cfg.Audio.Channels, cfg.Audio.FrameMs)
	if err != nil {
		_ = capture.Close()
		_ = pc.Close()
		return nil, fmt.Errorf("init opus encoder: %w", err)
	}

	return &SenderSession{
		pc:      pc,
		track:   track,
		capture: capture,
		encoder: encoder,
		logger:  logger,
		stopCh:  make(chan struct{}),
	}, nil
}

func (s *SenderSession) PeerConnection() *webrtc.PeerConnection {
	return s.pc
}

func (s *SenderSession) CreateOffer() (webrtc.SessionDescription, error) {
	offer, err := s.pc.CreateOffer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("create offer: %w", err)
	}
	if err := s.pc.SetLocalDescription(offer); err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("set local description: %w", err)
	}
	return offer, nil
}

func (s *SenderSession) SetRemoteAnswer(answer webrtc.SessionDescription) error {
	if err := s.pc.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("set remote answer: %w", err)
	}
	return nil
}

func (s *SenderSession) StartStreaming(ctx context.Context) {
	frameDuration := time.Duration(s.encoder.FrameMs()) * time.Millisecond

	go func() {
		ticker := time.NewTicker(frameDuration)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				pcm, err := s.capture.ReadFrame()
				if err != nil {
					s.logger.Error("audio capture error", "err", err)
					continue
				}

				opusFrame, err := s.encoder.Encode(pcm)
				if err != nil {
					s.logger.Error("opus encode error", "err", err)
					continue
				}

				if err := s.track.WriteSample(media.Sample{
					Data:     opusFrame,
					Duration: frameDuration,
				}); err != nil && err != io.EOF {
					s.logger.Error("write sample error", "err", err)
				}
			}
		}
	}()
}

func (s *SenderSession) Close() error {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	if s.capture != nil {
		_ = s.capture.Close()
	}
	if s.pc != nil {
		return s.pc.Close()
	}
	return nil
}

func NewReceiverSession(cfg PeerConfig, logger *slog.Logger) (*ReceiverSession, error) {
	if logger == nil {
		logger = slog.Default()
	}

	pc, err := NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}

	playback, err := audio.NewPlayback(audio.PlaybackConfig{
		SampleRate: cfg.Audio.SampleRate,
		Channels:   cfg.Audio.Channels,
	})
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("init playback: %w", err)
	}

	decoder, err := codec.NewDecoder(cfg.Audio.SampleRate, cfg.Audio.Channels, cfg.Audio.FrameMs)
	if err != nil {
		_ = playback.Close()
		_ = pc.Close()
		return nil, fmt.Errorf("init opus decoder: %w", err)
	}

	session := &ReceiverSession{
		pc:       pc,
		playback: playback,
		decoder:  decoder,
		logger:   logger,
		stopCh:   make(chan struct{}),
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		logger.Info("Receiving audio track", "id", track.ID())
		go session.readTrack(track)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logger.Info("WebRTC connection state", "state", state.String())
		if state == webrtc.PeerConnectionStateDisconnected ||
			state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed {
			playback.Reset()
		}
	})

	return session, nil
}

func (r *ReceiverSession) readTrack(track *webrtc.TrackRemote) {
	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			if err == io.EOF {
				r.playback.Reset()
				return
			}
			r.logger.Error("read rtp", "err", err)
			r.playback.Reset()
			return
		}

		pcm, err := r.decoder.Decode(rtpPacket.Payload)
		if err != nil {
			r.logger.Error("opus decode", "err", err)
			continue
		}

		if err := r.playback.WritePCM(pcm); err != nil {
			r.logger.Error("playback write", "err", err)
		}
	}
}

func (r *ReceiverSession) PeerConnection() *webrtc.PeerConnection {
	return r.pc
}

func (r *ReceiverSession) CreateAnswer(offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	if err := r.pc.SetRemoteDescription(offer); err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("set remote offer: %w", err)
	}

	answer, err := r.pc.CreateAnswer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("create answer: %w", err)
	}
	if err := r.pc.SetLocalDescription(answer); err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("set local description: %w", err)
	}
	return answer, nil
}

func (r *ReceiverSession) Close() error {
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})
	if r.playback != nil {
		_ = r.playback.Close()
	}
	if r.pc != nil {
		return r.pc.Close()
	}
	return nil
}

func WireICECandidates(
	ctx context.Context,
	pc *webrtc.PeerConnection,
	sig *signaling.Client,
	callID string,
	fromSender bool,
	logger *slog.Logger,
) {
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		jsonCandidate := c.ToJSON()
		var mline *int64
		if jsonCandidate.SDPMLineIndex != nil {
			v := int64(*jsonCandidate.SDPMLineIndex)
			mline = &v
		}
		candidate := signaling.ICECandidate{
			ID:            fmt.Sprintf("%d", time.Now().UnixNano()),
			Candidate:     jsonCandidate.Candidate,
			SDPMLineIndex: mline,
			SDPMid:      derefString(jsonCandidate.SDPMid),
		}
		if err := sig.AddICECandidate(ctx, callID, fromSender, candidate); err != nil {
			logger.Error("publish ice candidate", "err", err)
		}
	})

	go func() {
		err := sig.WatchICECandidates(ctx, callID, fromSender, func(candidate signaling.ICECandidate) {
			var mline *uint16
			if candidate.SDPMLineIndex != nil {
				v := uint16(*candidate.SDPMLineIndex)
				mline = &v
			}
			ice := webrtc.ICECandidateInit{
				Candidate:     candidate.Candidate,
				SDPMLineIndex: mline,
			}
			if candidate.SDPMid != "" {
				mid := candidate.SDPMid
				ice.SDPMid = &mid
			}
			if err := pc.AddICECandidate(ice); err != nil {
				logger.Warn("add ice candidate", "err", err)
			}
		})
		if err != nil && ctx.Err() == nil {
			logger.Error("watch ice candidates", "err", err)
		}
	}()
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func WaitForConnection(ctx context.Context, pc *webrtc.PeerConnection, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		state := pc.ConnectionState()
		switch state {
		case webrtc.PeerConnectionStateConnected:
			return nil
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			return fmt.Errorf("connection %s", state.String())
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("connection timeout after %s", timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
