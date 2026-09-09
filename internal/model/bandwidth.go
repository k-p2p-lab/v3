package model

import "time"

// BandwidthScope counts libp2p stream reads/writes, including serialized data,
// forwarding and control protocols. It excludes lower-layer framing, encryption,
// IP/TCP headers, retransmissions and the HTTP management/telemetry plane.
const BandwidthScope = "libp2p-stream-v1"

type BandwidthProtocol struct {
	Protocol      string `json:"protocol"`
	ReceivedBytes int64  `json:"receivedBytes"`
	SentBytes     int64  `json:"sentBytes"`
}

// Cumulative counters start at host creation and reset only with a new telemetry
// session. ElapsedNS uses the local monotonic clock, unaffected by clock sync.
type BandwidthSample struct {
	ElapsedNS     int64               `json:"elapsedNs"`
	ReceivedBytes int64               `json:"receivedBytes"`
	SentBytes     int64               `json:"sentBytes"`
	Protocols     []BandwidthProtocol `json:"protocols"`
	Final         bool                `json:"final"`
}

// BandwidthRates is attached only to live snapshots. ReportingSessions counts
// fresh non-final sessions; stale sessions are excluded from the summed rates.
type BandwidthRates struct {
	Available             bool    `json:"available"`
	SentBitsPerSecond     float64 `json:"sentBitsPerSecond"`
	ReceivedBitsPerSecond float64 `json:"receivedBitsPerSecond"`
	ReportingSessions     int     `json:"reportingSessions"`
	StaleSessions         int     `json:"staleSessions"`
}

type BandwidthSummary struct {
	CurrentRates      *BandwidthRates     `json:"currentRates,omitempty"`
	Scope             string              `json:"scope"`
	Sessions          int                 `json:"sessions"`
	FinalizedSessions int                 `json:"finalizedSessions"`
	Samples           int                 `json:"samples"`
	RejectedSamples   int                 `json:"rejectedSamples"`
	SupersededSamples int                 `json:"supersededSamples"`
	ReceivedBytes     int64               `json:"receivedBytes"`
	SentBytes         int64               `json:"sentBytes"`
	Protocols         []BandwidthProtocol `json:"protocols"`
	LatestAt          time.Time           `json:"latestAt"`
}
