package peer

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elecbug/kpl-v3/internal/model"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	libcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

const dhtProtocol = "/kpl/kad/1.0.0"

type Server struct {
	config     model.PeerProcessConfig
	host       host.Host
	dht        *dht.IpfsDHT
	pubsub     *pubsub.PubSub
	topics     map[string]*pubsub.Topic
	subs       []*pubsub.Subscription
	telemetry  *telemetry
	logger     *slog.Logger
	startedAt  time.Time
	publishSeq atomic.Uint64
	closeOnce  sync.Once
}

type envelope struct {
	ID        string `json:"id"`
	RunID     string `json:"runId"`
	Publisher string `json:"publisher"`
	SentAt    int64  `json:"sentAt"`
	Payload   []byte `json:"payload"`
}

type bootstrapNode struct {
	NodeID    string   `json:"nodeId"`
	PeerID    string   `json:"peerId"`
	Addresses []string `json:"addresses"`
}

func Run(ctx context.Context, configPath string, logger *slog.Logger) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read peer config: %w", err)
	}
	var config model.PeerProcessConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("decode peer config: %w", err)
	}
	server, err := New(ctx, config, logger)
	if err != nil {
		return err
	}
	return server.Run(ctx)
}

func New(ctx context.Context, config model.PeerProcessConfig, logger *slog.Logger) (*Server, error) {
	if config.Node.ID == "" || config.Node.RunID == "" || config.AgentURL == "" || config.ControllerURL == "" {
		return nil, fmt.Errorf("peer config requires node, run, agent, and controller")
	}
	if logger == nil {
		logger = slog.Default()
	}
	config.NodeConfig = config.NodeConfig.WithDefaults()
	identity, err := deterministicIdentity(config.Seed)
	if err != nil {
		return nil, fmt.Errorf("create identity: %w", err)
	}
	h, err := libp2p.New(libp2p.Identity(identity), libp2p.ListenAddrStrings(config.P2PListen))
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}
	telemetry := newTelemetry(config.Node, config.AgentURL, config.Token, logger)
	server := &Server{
		config:    config,
		host:      h,
		topics:    make(map[string]*pubsub.Topic),
		telemetry: telemetry,
		logger:    logger,
		startedAt: time.Now().UTC(),
	}
	dhtInstance, err := dht.New(ctx, h, dht.Mode(dht.ModeServer), dht.ProtocolPrefix(protocol.ID(dhtProtocol)))
	if err != nil {
		_ = h.Close()
		return nil, fmt.Errorf("create dht: %w", err)
	}
	server.dht = dhtInstance
	return server, nil
}

func (s *Server) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.Close()
	go s.telemetry.run(ctx)
	if err := s.connectBootstrap(ctx); err != nil {
		return err
	}
	if err := s.dht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrap dht: %w", err)
	}
	params := pubsub.DefaultGossipSubParams()
	params.D = s.config.NodeConfig.D
	params.Dlo = s.config.NodeConfig.DLow
	params.Dhi = s.config.NodeConfig.DHigh
	params.Dout = s.config.NodeConfig.DOut
	params.Dlazy = s.config.NodeConfig.DLazy
	if heartbeat, err := time.ParseDuration(s.config.NodeConfig.Heartbeat); err == nil && heartbeat > 0 {
		params.HeartbeatInterval = heartbeat
	}
	tracer := &gossipTracer{telemetry: s.telemetry, peerID: s.host.ID().String()}
	ps, err := pubsub.NewGossipSub(ctx, s.host, pubsub.WithGossipSubParams(params), pubsub.WithEventTracer(tracer))
	if err != nil {
		return fmt.Errorf("create gossipsub: %w", err)
	}
	s.pubsub = ps
	for _, topicName := range s.config.NodeConfig.Topics {
		topic, err := ps.Join(topicName)
		if err != nil {
			return fmt.Errorf("join topic %s: %w", topicName, err)
		}
		sub, err := topic.Subscribe()
		if err != nil {
			return fmt.Errorf("subscribe topic %s: %w", topicName, err)
		}
		s.topics[topicName] = topic
		s.subs = append(s.subs, sub)
		go s.consume(ctx, topicName, sub)
	}
	apiServer := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	listener, err := net.Listen("tcp", s.config.APListen)
	if err != nil {
		return fmt.Errorf("listen on peer api: %w", err)
	}
	if err := s.reportStatus(ctx, model.NodeReady, ""); err != nil {
		_ = listener.Close()
		return fmt.Errorf("report ready status: %w", err)
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = apiServer.Shutdown(shutdownCtx)
	}()
	go s.statusLoop(ctx)
	s.logger.Info("peer ready", "node", s.config.Node.ID, "peer", s.host.ID(), "api", s.config.APListen)
	err = apiServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready", "nodeId": s.config.Node.ID, "peerId": s.host.ID().String()})
	})
	mux.HandleFunc("/publish", s.handlePublish)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config.Token != "" && r.Method != http.MethodGet && r.Header.Get("Authorization") != "Bearer "+s.config.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request model.PublishRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if request.PayloadSize <= 0 {
		request.PayloadSize = 32
	}
	if request.PayloadSize > 16<<20 {
		http.Error(w, "payload exceeds 16 MiB", http.StatusBadRequest)
		return
	}
	topicName := request.Topic
	if topicName == "" {
		topicName = s.config.NodeConfig.Topics[0]
	}
	topic, ok := s.topics[topicName]
	if !ok {
		http.Error(w, "unknown topic", http.StatusBadRequest)
		return
	}
	payload := make([]byte, request.PayloadSize)
	if _, err := crand.Read(payload); err != nil {
		http.Error(w, "cannot generate payload", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	sequence := s.publishSeq.Add(1)
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", s.config.Node.ID, now.UnixNano(), sequence)))
	message := envelope{ID: hex.EncodeToString(digest[:16]), RunID: s.config.Node.RunID, Publisher: s.config.Node.ID, SentAt: now.UnixNano(), Payload: payload}
	wire, err := json.Marshal(message)
	if err != nil {
		http.Error(w, "cannot encode message", http.StatusInternalServerError)
		return
	}
	publishCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := topic.Publish(publishCtx, wire); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.telemetry.emit(model.TraceEvent{PeerID: s.host.ID().String(), Type: "publish", MessageID: message.ID, Topic: topicName, Timestamp: now, Fields: map[string]any{"payloadBytes": request.PayloadSize, "wireBytes": len(wire), "targetNodes": request.TargetNodes}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"messageId": message.ID, "topic": topicName, "payloadBytes": request.PayloadSize})
}

func (s *Server) consume(ctx context.Context, topic string, sub *pubsub.Subscription) {
	for {
		message, err := sub.Next(ctx)
		if err != nil {
			return
		}
		var payload envelope
		if err := json.Unmarshal(message.Data, &payload); err != nil {
			s.telemetry.emit(model.TraceEvent{PeerID: s.host.ID().String(), Type: "reject", Topic: topic, Timestamp: time.Now().UTC(), Fields: map[string]any{"reason": "invalid-envelope"}})
			continue
		}
		if payload.RunID != s.config.Node.RunID {
			continue
		}
		now := time.Now().UTC()
		latency := float64(now.UnixNano()-payload.SentAt) / float64(time.Millisecond)
		fields := map[string]any{"publisher": payload.Publisher, "payloadBytes": len(payload.Payload)}
		if latency < 0 {
			fields["clockSkewDetected"] = true
		}
		s.telemetry.emit(model.TraceEvent{PeerID: s.host.ID().String(), Type: "deliver", MessageID: payload.ID, RemotePeerID: message.ReceivedFrom.String(), Topic: topic, Timestamp: now, LatencyMS: latency, Fields: fields})
	}
}

func (s *Server) connectBootstrap(ctx context.Context) error {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	for {
		nodes, err := s.fetchBootstrap(ctx)
		if err == nil {
			connected := 0
			for _, node := range nodes {
				if node.PeerID == s.host.ID().String() {
					continue
				}
				info, err := addrInfo(node)
				if err != nil {
					continue
				}
				connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err = s.host.Connect(connectCtx, info)
				cancel()
				if err == nil {
					connected++
				}
			}
			if s.config.Node.Role == "boot" || connected > 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("no reachable bootstrap node after 60s")
		case <-time.After(time.Second):
		}
	}
}

func (s *Server) fetchBootstrap(ctx context.Context) ([]bootstrapNode, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.config.ControllerURL, "/")+"/api/v1/bootstrap", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bootstrap registry returned %s", resp.Status)
	}
	var nodes []bootstrapNode
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func addrInfo(node bootstrapNode) (peer.AddrInfo, error) {
	id, err := peer.Decode(node.PeerID)
	if err != nil {
		return peer.AddrInfo{}, err
	}
	info := peer.AddrInfo{ID: id}
	for _, raw := range node.Addresses {
		address, err := multiaddr.NewMultiaddr(raw)
		if err == nil {
			info.Addrs = append(info.Addrs, address)
		}
	}
	if len(info.Addrs) == 0 {
		return peer.AddrInfo{}, fmt.Errorf("bootstrap node has no valid address")
	}
	return info, nil
}

func (s *Server) statusLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			if err := s.reportStatus(statusCtx, model.NodeReady, ""); err != nil {
				s.logger.Warn("report peer status", "error", err)
			}
			cancel()
		}
	}
}

func (s *Server) reportStatus(ctx context.Context, state, message string) error {
	node := s.config.Node
	node.PeerID = s.host.ID().String()
	node.State = state
	node.Error = message
	node.StartedAt = s.startedAt
	node.LastSeen = time.Now().UTC()
	for _, address := range s.host.Addrs() {
		node.Addresses = append(node.Addresses, address.String())
	}
	for _, connection := range s.host.Network().Conns() {
		node.ConnectedPeers = append(node.ConnectedPeers, connection.RemotePeer().String())
	}
	sort.Strings(node.ConnectedPeers)
	node.ConnectedPeers = unique(node.ConnectedPeers)
	node.TopicPeers = make(map[string]int)
	if s.pubsub != nil {
		for topic := range s.topics {
			node.TopicPeers[topic] = len(s.pubsub.ListPeers(topic))
		}
	}
	return s.telemetry.reportNode(ctx, node)
}

func deterministicIdentity(seed int64) (libcrypto.PrivKey, error) {
	if seed == 0 {
		seed = 1
	}
	rng := mrand.New(mrand.NewSource(seed))
	privateKey, _, err := libcrypto.GenerateEd25519Key(rng)
	return privateKey, err
}

func unique(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func (s *Server) Close() {
	s.closeOnce.Do(func() {
		for _, sub := range s.subs {
			sub.Cancel()
		}
		for _, topic := range s.topics {
			_ = topic.Close()
		}
		if s.dht != nil {
			_ = s.dht.Close()
		}
		if s.host != nil {
			_ = s.host.Close()
		}
	})
}
