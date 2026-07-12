package receiver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	pion "github.com/pion/webrtc/v4"

	"github.com/streamd/streamd/internal/config"
	"github.com/streamd/streamd/internal/signaling"
	streamwebrtc "github.com/streamd/streamd/internal/webrtc"
)

const connectionTimeout = 30 * time.Second

type Client struct {
	cfg    *config.Config
	logger *slog.Logger
	sig    *signaling.Client
}

func NewClient(cfg *config.Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{cfg: cfg, logger: logger}
}

func (c *Client) Connect(ctx context.Context, senderID string) error {
	sig, err := signaling.NewClient(ctx, c.cfg.Firebase.ProjectID, c.cfg.Firebase.CredentialsFile, c.logger)
	if err != nil {
		return fmt.Errorf("connect firebase: %w", err)
	}
	c.sig = sig
	defer sig.Close()

	online, err := sig.IsPeerOnline(ctx, senderID)
	if err != nil {
		return err
	}
	if !online {
		return fmt.Errorf("peer %q is not online", senderID)
	}

	callID := fmt.Sprintf("%s-%s-%d", c.cfg.PeerID, senderID, time.Now().UnixNano())
	if err := sig.CreateCall(ctx, callID, senderID, c.cfg.PeerID); err != nil {
		return fmt.Errorf("create call: %w", err)
	}
	c.logger.Info("Call created", "call", callID, "sender", senderID)

	peerCfg := streamwebrtc.PeerConfig{
		STUNServers: c.cfg.WebRTC.STUNServers,
		Audio:       c.cfg.Audio,
	}

	session, err := streamwebrtc.NewReceiverSession(peerCfg, c.logger)
	if err != nil {
		_ = sig.SetCallStatus(ctx, callID, signaling.StatusFailed)
		return fmt.Errorf("create receiver session: %w", err)
	}
	defer session.Close()

	streamwebrtc.WireICECandidates(ctx, session.PeerConnection(), sig, callID, false, c.logger)

	offerCh := make(chan string, 1)
	go func() {
		_ = sig.WatchCall(ctx, callID, func(call *signaling.Call) {
			if call.Offer != "" {
				select {
				case offerCh <- call.Offer:
				default:
				}
			}
		})
	}()

	var offerSDP string
	select {
	case <-ctx.Done():
		return ctx.Err()
	case offerSDP = <-offerCh:
	}

	offer := pion.SessionDescription{Type: pion.SDPTypeOffer, SDP: offerSDP}
	answer, err := session.CreateAnswer(offer)
	if err != nil {
		_ = sig.SetCallStatus(ctx, callID, signaling.StatusFailed)
		return err
	}

	if err := sig.SetAnswer(ctx, callID, answer.SDP); err != nil {
		_ = sig.SetCallStatus(ctx, callID, signaling.StatusFailed)
		return err
	}

	if err := streamwebrtc.WaitForConnection(ctx, session.PeerConnection(), connectionTimeout); err != nil {
		_ = sig.SetCallStatus(ctx, callID, signaling.StatusFailed)
		return err
	}

	c.logger.Info("WebRTC connected")
	_ = sig.SetCallStatus(ctx, callID, signaling.StatusConnected)
	c.logger.Info("Playing audio (press Ctrl+C to stop)")

	<-ctx.Done()
	_ = sig.SetCallStatus(context.Background(), callID, signaling.StatusClosed)
	return ctx.Err()
}

func (c *Client) ListPeers(ctx context.Context) (map[string]time.Time, error) {
	sig, err := signaling.NewClient(ctx, c.cfg.Firebase.ProjectID, c.cfg.Firebase.CredentialsFile, c.logger)
	if err != nil {
		return nil, fmt.Errorf("connect firebase: %w", err)
	}
	defer sig.Close()

	return sig.ListOnlinePeers(ctx)
}
