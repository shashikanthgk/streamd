package signaling

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	CollectionPeers = "peers"
	CollectionCalls = "calls"

	SubSenderCandidates   = "senderCandidates"
	SubReceiverCandidates = "receiverCandidates"

	StatusPending    = "pending"
	StatusOfferSent  = "offer_sent"
	StatusAnswerSent = "answer_sent"
	StatusConnected  = "connected"
	StatusFailed     = "failed"
	StatusClosed     = "closed"
)

type Call struct {
	ID         string
	SenderID   string
	ReceiverID string
	Offer      string
	Answer     string
	Status     string
}

type ICECandidate struct {
	ID          string
	Candidate   string
	SDPMLineIndex *int64
	SDPMid      string
}

type Client struct {
	projectID string
	client    *firestore.Client
	logger    *slog.Logger
	mu        sync.Mutex
}

func NewClient(ctx context.Context, projectID, credentialsFile string, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	conf := &firebase.Config{ProjectID: projectID}
	app, err := firebase.NewApp(ctx, conf, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("init firebase app: %w", err)
	}

	fsClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("init firestore client: %w", err)
	}

	return &Client{
		projectID: projectID,
		client:    fsClient,
		logger:    logger,
	}, nil
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) RegisterPeer(ctx context.Context, peerID string) error {
	_, err := c.client.Collection(CollectionPeers).Doc(peerID).Set(ctx, map[string]interface{}{
		"online":   true,
		"lastSeen": firestore.ServerTimestamp,
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("register peer: %w", err)
	}
	return nil
}

func (c *Client) Heartbeat(ctx context.Context, peerID string) error {
	_, err := c.client.Collection(CollectionPeers).Doc(peerID).Set(ctx, map[string]interface{}{
		"online":   true,
		"lastSeen": firestore.ServerTimestamp,
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	return nil
}

func (c *Client) UnregisterPeer(ctx context.Context, peerID string) error {
	_, err := c.client.Collection(CollectionPeers).Doc(peerID).Set(ctx, map[string]interface{}{
		"online":   false,
		"lastSeen": firestore.ServerTimestamp,
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("unregister peer: %w", err)
	}
	return nil
}

func (c *Client) ListOnlinePeers(ctx context.Context) (map[string]time.Time, error) {
	docs, err := c.client.Collection(CollectionPeers).Where("online", "==", true).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}

	peers := make(map[string]time.Time, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		if ts, ok := data["lastSeen"].(time.Time); ok {
			peers[doc.Ref.ID] = ts
		} else {
			peers[doc.Ref.ID] = time.Now()
		}
	}
	return peers, nil
}

func (c *Client) IsPeerOnline(ctx context.Context, peerID string) (bool, error) {
	doc, err := c.client.Collection(CollectionPeers).Doc(peerID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return false, nil
		}
		return false, fmt.Errorf("get peer: %w", err)
	}
	data := doc.Data()
	online, _ := data["online"].(bool)
	return online, nil
}

func (c *Client) CreateCall(ctx context.Context, callID, senderID, receiverID string) error {
	_, err := c.client.Collection(CollectionCalls).Doc(callID).Set(ctx, map[string]interface{}{
		"senderID":   senderID,
		"receiverID": receiverID,
		"offer":      "",
		"answer":     "",
		"status":     StatusPending,
		"createdAt":  firestore.ServerTimestamp,
	})
	if err != nil {
		return fmt.Errorf("create call: %w", err)
	}
	return nil
}

func (c *Client) SetOffer(ctx context.Context, callID, offer string) error {
	_, err := c.client.Collection(CollectionCalls).Doc(callID).Set(ctx, map[string]interface{}{
		"offer":  offer,
		"status": StatusOfferSent,
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("set offer: %w", err)
	}
	return nil
}

func (c *Client) SetAnswer(ctx context.Context, callID, answer string) error {
	_, err := c.client.Collection(CollectionCalls).Doc(callID).Set(ctx, map[string]interface{}{
		"answer": answer,
		"status": StatusAnswerSent,
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("set answer: %w", err)
	}
	return nil
}

func (c *Client) SetCallStatus(ctx context.Context, callID, status string) error {
	_, err := c.client.Collection(CollectionCalls).Doc(callID).Set(ctx, map[string]interface{}{
		"status": status,
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("set call status: %w", err)
	}
	return nil
}

func (c *Client) GetCall(ctx context.Context, callID string) (*Call, error) {
	doc, err := c.client.Collection(CollectionCalls).Doc(callID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("get call: %w", err)
	}
	return callFromDoc(doc)
}

func (c *Client) AddICECandidate(ctx context.Context, callID string, fromSender bool, candidate ICECandidate) error {
	sub := SubReceiverCandidates
	if fromSender {
		sub = SubSenderCandidates
	}

	data := map[string]interface{}{
		"candidate":     candidate.Candidate,
		"sdpMid":        candidate.SDPMid,
		"sdpMLineIndex": candidate.SDPMLineIndex,
		"createdAt":     firestore.ServerTimestamp,
	}

	id := candidate.ID
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	_, err := c.client.Collection(CollectionCalls).Doc(callID).Collection(sub).Doc(id).Set(ctx, data)
	if err != nil {
		return fmt.Errorf("add ice candidate: %w", err)
	}
	return nil
}

func (c *Client) WatchIncomingCalls(ctx context.Context, senderID string, handler func(*Call)) error {
	it := c.client.Collection(CollectionCalls).
		Where("senderID", "==", senderID).
		Where("status", "==", StatusPending).
		Snapshots(ctx)

	for {
		snap, err := it.Next()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("watch incoming calls: %w", err)
		}

		for _, change := range snap.Changes {
			if change.Kind == firestore.DocumentAdded || change.Kind == firestore.DocumentModified {
				call, err := callFromDoc(change.Doc)
				if err != nil {
					c.logger.Warn("skip invalid call document", "err", err)
					continue
				}
				if call.Status == StatusPending {
					handler(call)
				}
			}
		}
	}
}

func (c *Client) WatchCall(ctx context.Context, callID string, handler func(*Call)) error {
	it := c.client.Collection(CollectionCalls).Doc(callID).Snapshots(ctx)

	for {
		snap, err := it.Next()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("watch call: %w", err)
		}

		if !snap.Exists() {
			continue
		}

		call, err := callFromDoc(snap)
		if err != nil {
			c.logger.Warn("skip invalid call snapshot", "err", err)
			continue
		}
		handler(call)
	}
}

func (c *Client) WatchICECandidates(ctx context.Context, callID string, fromSender bool, handler func(ICECandidate)) error {
	sub := SubSenderCandidates
	if fromSender {
		sub = SubReceiverCandidates
	}

	it := c.client.Collection(CollectionCalls).Doc(callID).Collection(sub).Snapshots(ctx)
	seen := make(map[string]struct{})

	for {
		snap, err := it.Next()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("watch ice candidates: %w", err)
		}

		for _, change := range snap.Changes {
			if change.Kind != firestore.DocumentAdded && change.Kind != firestore.DocumentModified {
				continue
			}
			id := change.Doc.Ref.ID
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}

			data := change.Doc.Data()
			candidate, err := iceFromData(id, data)
			if err != nil {
				c.logger.Warn("skip invalid ice candidate", "err", err)
				continue
			}
			handler(candidate)
		}
	}
}

func callFromDoc(doc *firestore.DocumentSnapshot) (*Call, error) {
	data := doc.Data()
	call := &Call{
		ID:         doc.Ref.ID,
		SenderID:   stringField(data, "senderID"),
		ReceiverID: stringField(data, "receiverID"),
		Offer:      stringField(data, "offer"),
		Answer:     stringField(data, "answer"),
		Status:     stringField(data, "status"),
	}
	return call, nil
}

func iceFromData(id string, data map[string]interface{}) (ICECandidate, error) {
	candidate := stringField(data, "candidate")
	if candidate == "" {
		return ICECandidate{}, fmt.Errorf("empty candidate")
	}

	var mline *int64
	switch v := data["sdpMLineIndex"].(type) {
	case int64:
		mline = &v
	case float64:
		i := int64(v)
		mline = &i
	}

	return ICECandidate{
		ID:            id,
		Candidate:     candidate,
		SDPMLineIndex: mline,
		SDPMid:      stringField(data, "sdpMid"),
	}, nil
}

func stringField(data map[string]interface{}, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}
