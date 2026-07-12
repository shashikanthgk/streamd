package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	pion "github.com/pion/webrtc/v4"

	"github.com/streamd/streamd/internal/config"
	"github.com/streamd/streamd/internal/signaling"
	streamwebrtc "github.com/streamd/streamd/internal/webrtc"
)

const (
	heartbeatInterval   = 15 * time.Second
	connectionTimeout   = 30 * time.Second
	reconnectBaseDelay  = 2 * time.Second
	maxReconnectDelay   = 30 * time.Second
)

type Daemon struct {
	cfg    *config.Config
	logger *slog.Logger
	sig    *signaling.Client

	mu          sync.Mutex
	activeCall  string
	activeSess  *streamwebrtc.SenderSession
}

func New(cfg *config.Config, logger *slog.Logger) *Daemon {
	if logger == nil {
		logger = slog.Default()
	}
	return &Daemon{cfg: cfg, logger: logger}
}

func (d *Daemon) Run(ctx context.Context) error {
	sig, err := signaling.NewClient(ctx, d.cfg.Firebase.ProjectID, d.cfg.Firebase.CredentialsFile, d.logger)
	if err != nil {
		return fmt.Errorf("connect firebase: %w", err)
	}
	d.sig = sig
	defer sig.Close()

	d.logger.Info("Firebase connected")

	if err := sig.RegisterPeer(ctx, d.cfg.PeerID); err != nil {
		return fmt.Errorf("register peer: %w", err)
	}
	d.logger.Info("Peer ID", "id", d.cfg.PeerID)
	d.logger.Info("Waiting for connections")

	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go d.runHeartbeat(heartbeatCtx)

	defer func() {
		_ = sig.UnregisterPeer(context.Background(), d.cfg.PeerID)
	}()

	for {
		if ctx.Err() != nil {
			d.closeActiveSession()
			return ctx.Err()
		}

		err := d.waitForConnection(ctx)
		if err != nil {
			if ctx.Err() != nil {
				d.closeActiveSession()
				return ctx.Err()
			}
			d.logger.Error("Connection session ended", "err", err)
			d.closeActiveSession()
			d.sleepWithContext(ctx, reconnectBaseDelay)
		}
	}
}

func (d *Daemon) runHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.sig.Heartbeat(ctx, d.cfg.PeerID); err != nil {
				d.logger.Warn("Heartbeat failed", "err", err)
			}
		}
	}
}

func (d *Daemon) waitForConnection(ctx context.Context) error {
	callCh := make(chan *signaling.Call, 1)

	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()

	go func() {
		err := d.sig.WatchIncomingCalls(watchCtx, d.cfg.PeerID, func(call *signaling.Call) {
			select {
			case callCh <- call:
			default:
			}
		})
		if err != nil && watchCtx.Err() == nil {
			d.logger.Error("Watch incoming calls failed", "err", err)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case call := <-callCh:
		d.logger.Info("Incoming connection", "call", call.ID, "receiver", call.ReceiverID)
		return d.handleCall(ctx, call)
	}
}

func (d *Daemon) handleCall(ctx context.Context, call *signaling.Call) error {
	peerCfg := streamwebrtc.PeerConfig{
		STUNServers: d.cfg.WebRTC.STUNServers,
		Audio:       d.cfg.Audio,
	}

	session, err := streamwebrtc.NewSenderSession(peerCfg, d.logger)
	if err != nil {
		_ = d.sig.SetCallStatus(ctx, call.ID, signaling.StatusFailed)
		return fmt.Errorf("create sender session: %w", err)
	}

	d.mu.Lock()
	d.activeCall = call.ID
	d.activeSess = session
	d.mu.Unlock()

	defer d.closeActiveSession()

	callCtx, cancelCall := context.WithCancel(ctx)
	defer cancelCall()

	streamwebrtc.WireICECandidates(callCtx, session.PeerConnection(), d.sig, call.ID, true, d.logger)

	offer, err := session.CreateOffer()
	if err != nil {
		_ = d.sig.SetCallStatus(ctx, call.ID, signaling.StatusFailed)
		return err
	}

	if err := d.sig.SetOffer(ctx, call.ID, offer.SDP); err != nil {
		_ = d.sig.SetCallStatus(ctx, call.ID, signaling.StatusFailed)
		return err
	}

	answerCh := make(chan string, 1)
	go func() {
		_ = d.sig.WatchCall(callCtx, call.ID, func(updated *signaling.Call) {
			if updated.Answer != "" {
				select {
				case answerCh <- updated.Answer:
				default:
				}
			}
		})
	}()

	var answerSDP string
	select {
	case <-callCtx.Done():
		return callCtx.Err()
	case answerSDP = <-answerCh:
	}

	answer := pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answerSDP}
	if err := session.SetRemoteAnswer(answer); err != nil {
		_ = d.sig.SetCallStatus(ctx, call.ID, signaling.StatusFailed)
		return err
	}

	if err := streamwebrtc.WaitForConnection(callCtx, session.PeerConnection(), connectionTimeout); err != nil {
		_ = d.sig.SetCallStatus(ctx, call.ID, signaling.StatusFailed)
		return err
	}

	d.logger.Info("WebRTC connected")
	_ = d.sig.SetCallStatus(ctx, call.ID, signaling.StatusConnected)
	d.logger.Info("Streaming audio")

	session.StartStreaming(callCtx)

	session.PeerConnection().OnConnectionStateChange(func(state pion.PeerConnectionState) {
		if state == pion.PeerConnectionStateDisconnected || state == pion.PeerConnectionStateFailed || state == pion.PeerConnectionStateClosed {
			cancelCall()
		}
	})

	<-callCtx.Done()
	_ = d.sig.SetCallStatus(context.Background(), call.ID, signaling.StatusClosed)
	return callCtx.Err()
}

func (d *Daemon) closeActiveSession() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.activeSess != nil {
		_ = d.activeSess.Close()
		d.activeSess = nil
	}
	d.activeCall = ""
}

func (d *Daemon) sleepWithContext(ctx context.Context, dura time.Duration) {
	timer := time.NewTimer(dura)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func SetupLogger(level string, logFile string) (*slog.Logger, error) {
	var levelVar slog.Level
	switch level {
	case "debug":
		levelVar = slog.LevelDebug
	case "warn":
		levelVar = slog.LevelWarn
	case "error":
		levelVar = slog.LevelError
	default:
		levelVar = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: levelVar}
	var handler slog.Handler = slog.NewTextHandler(os.Stdout, opts)

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		fileHandler := slog.NewTextHandler(f, opts)
		handler = newMultiHandler(handler, fileHandler)
	}

	return slog.New(handler), nil
}

type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) slog.Handler {
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if err := h.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: out}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: out}
}
