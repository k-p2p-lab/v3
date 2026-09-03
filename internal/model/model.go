package model

import "time"

const (
	AgentOnline  = "online"
	AgentOffline = "offline"

	NodeStarting = "starting"
	NodeReady    = "ready"
	NodeStopping = "stopping"
	NodeStopped  = "stopped"
	NodeFailed   = "failed"
)

type Agent struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	URL         string            `json:"url"`
	Hostname    string            `json:"hostname"`
	Version     string            `json:"version"`
	Capacity    int               `json:"capacity"`
	ActiveNodes int               `json:"activeNodes"`
	State       string            `json:"state"`
	Labels      map[string]string `json:"labels,omitempty"`
	StartedAt   time.Time         `json:"startedAt"`
	LastSeen    time.Time         `json:"lastSeen"`
}

type Node struct {
	ID             string            `json:"id"`
	RunID          string            `json:"runId"`
	AgentID        string            `json:"agentId"`
	Group          string            `json:"group"`
	Role           string            `json:"role"`
	PeerID         string            `json:"peerId,omitempty"`
	State          string            `json:"state"`
	Addresses      []string          `json:"addresses,omitempty"`
	ConnectedPeers []string          `json:"connectedPeers,omitempty"`
	TopicPeers     map[string]int    `json:"topicPeers,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	StartedAt      time.Time         `json:"startedAt"`
	LastSeen       time.Time         `json:"lastSeen"`
	Error          string            `json:"error,omitempty"`
}

type TraceEvent struct {
	RunID        string         `json:"runId"`
	AgentID      string         `json:"agentId,omitempty"`
	NodeID       string         `json:"nodeId"`
	PeerID       string         `json:"peerId,omitempty"`
	Type         string         `json:"type"`
	MessageID    string         `json:"messageId,omitempty"`
	RemotePeerID string         `json:"remotePeerId,omitempty"`
	Topic        string         `json:"topic,omitempty"`
	Timestamp    time.Time      `json:"timestamp"`
	LatencyMS    float64        `json:"latencyMs,omitempty"`
	Fields       map[string]any `json:"fields,omitempty"`
}

type EventBatch struct {
	AgentID string       `json:"agentId"`
	Events  []TraceEvent `json:"events"`
}

type AgentHeartbeat struct {
	Agent Agent  `json:"agent"`
	Nodes []Node `json:"nodes"`
}

type NodeConfig struct {
	Topics       []string `json:"topics" yaml:"topics"`
	D            int      `json:"d" yaml:"d"`
	DLow         int      `json:"dLow" yaml:"dLow"`
	DHigh        int      `json:"dHigh" yaml:"dHigh"`
	DOut         int      `json:"dOut" yaml:"dOut"`
	DLazy        int      `json:"dLazy" yaml:"dLazy"`
	Heartbeat    string   `json:"heartbeat" yaml:"heartbeat"`
	ConnectLimit int      `json:"connectLimit" yaml:"connectLimit"`
}

func (c NodeConfig) WithDefaults() NodeConfig {
	if len(c.Topics) == 0 {
		c.Topics = []string{"kpl/default"}
	}
	if c.D == 0 {
		c.D = 6
	}
	if c.DLow == 0 {
		c.DLow = 4
	}
	if c.DHigh == 0 {
		c.DHigh = 12
	}
	if c.DOut == 0 {
		c.DOut = 2
	}
	if c.DLazy == 0 {
		c.DLazy = 6
	}
	if c.Heartbeat == "" {
		c.Heartbeat = "1s"
	}
	return c
}

type CreateNodeRequest struct {
	ID       string     `json:"id"`
	RunID    string     `json:"runId"`
	Group    string     `json:"group"`
	Role     string     `json:"role"`
	Seed     int64      `json:"seed"`
	Config   NodeConfig `json:"config"`
	Lifetime string     `json:"lifetime,omitempty"`
}

type PublishRequest struct {
	RunID       string `json:"runId"`
	Topic       string `json:"topic,omitempty"`
	PayloadSize int    `json:"payloadSize"`
	TargetNodes int    `json:"targetNodes,omitempty"`
}

type Experiment struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	State        string    `json:"state"`
	Seed         int64     `json:"seed"`
	Phase        int       `json:"phase"`
	TotalPhases  int       `json:"totalPhases"`
	PhaseName    string    `json:"phaseName,omitempty"`
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt,omitempty"`
	Error        string    `json:"error,omitempty"`
	ScenarioYAML string    `json:"-"`
}

type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type Metrics struct {
	Published        int     `json:"published"`
	Delivered        int     `json:"delivered"`
	Duplicates       int     `json:"duplicates"`
	AverageLatencyMS float64 `json:"averageLatencyMs"`
	P95LatencyMS     float64 `json:"p95LatencyMs"`
	Reachability     float64 `json:"reachability"`
}

type Snapshot struct {
	GeneratedAt time.Time    `json:"generatedAt"`
	Agents      []Agent      `json:"agents"`
	Nodes       []Node       `json:"nodes"`
	Experiments []Experiment `json:"experiments"`
	Edges       []Edge       `json:"edges"`
	Events      []TraceEvent `json:"events"`
	Metrics     Metrics      `json:"metrics"`
}

type PeerProcessConfig struct {
	Node          Node       `json:"node"`
	NodeConfig    NodeConfig `json:"nodeConfig"`
	Seed          int64      `json:"seed"`
	ControllerURL string     `json:"controllerUrl"`
	AgentURL      string     `json:"agentUrl"`
	APListen      string     `json:"apiListen"`
	P2PListen     string     `json:"p2pListen"`
	Token         string     `json:"token,omitempty"`
}
