package controller

import (
	"math"
	"sort"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

const sessionWindowDefinition = "session-window-v1"

type measurementSessionKey struct{ nodeID, sessionID string }
type sequenceRange struct{ first, last uint64 }

// Coalesce contiguous source sequence numbers. In-order sessions need one
// interval; out-of-order arrival costs space only for the remaining gaps.
type sequenceRanges []sequenceRange

func (ranges *sequenceRanges) add(sequence uint64) {
	if sequence == 0 {
		return
	}
	i := sort.Search(len(*ranges), func(i int) bool { return (*ranges)[i].last >= sequence || (*ranges)[i].last == sequence-1 })
	if i == len(*ranges) {
		*ranges = append(*ranges, sequenceRange{sequence, sequence})
		return
	}
	r := &(*ranges)[i]
	if sequence < r.first && r.first-sequence > 1 {
		*ranges = append(*ranges, sequenceRange{})
		copy((*ranges)[i+1:], (*ranges)[i:])
		(*ranges)[i] = sequenceRange{sequence, sequence}
		return
	}
	r.first, r.last = min(r.first, sequence), max(r.last, sequence)
	if i+1 < len(*ranges) && (*ranges)[i+1].first-r.last <= 1 {
		r.last = max(r.last, (*ranges)[i+1].last)
		*ranges = append((*ranges)[:i+1], (*ranges)[i+2:]...)
	}
}

func (ranges sequenceRanges) through(start uint64) uint64 {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].last >= start })
	if start == 0 || i == len(ranges) || ranges[i].first > start {
		return 0
	}
	return ranges[i].last
}

type sessionProof struct {
	at       time.Time
	sequence uint64
}

type measurementSession struct {
	key       measurementSessionKey
	started   bool
	start     sessionProof
	topics    map[string]struct{}
	known     bool
	sequences sequenceRanges
	proofs    []sessionProof
	stop      *sessionProof
	invalid   bool
}

type windowPublication struct {
	key          messageMetricKey
	publisher    string
	sessionID    string
	auditTargets []string
	at           time.Time
	deadline     time.Time
	valid        bool
}

type windowDelivery struct {
	proof sessionProof
	value deliveryMetric
}

type windowReceipts struct {
	deliveries []windowDelivery
	duplicates []sessionProof
}

type sessionWindowAccumulator struct {
	enabled      bool
	incomplete   bool
	sessions     map[measurementSessionKey]*measurementSession
	publications map[messageMetricKey]windowPublication
	receipts     map[messageMetricKey]map[measurementSessionKey]*windowReceipts
	terminations map[string][]time.Time
}

func (w *sessionWindowAccumulator) observe(event model.TraceEvent) {
	if w.sessions == nil {
		w.sessions = make(map[measurementSessionKey]*measurementSession)
		w.publications = make(map[messageMetricKey]windowPublication)
		w.receipts = make(map[messageMetricKey]map[measurementSessionKey]*windowReceipts)
		w.terminations = make(map[string][]time.Time)
	}
	if event.Type == "measurement_terminated" {
		// Confirmed removal is an end upper bound, never proof of liveness or
		// of a complete receiver event stream. It has no Peer sequence number.
		if event.NodeID != "" && !event.Timestamp.IsZero() {
			w.terminations[event.NodeID] = append(w.terminations[event.NodeID], event.Timestamp)
		}
		return
	}
	definition, _ := event.Fields["measurementDefinition"].(string)
	isLifecycle := event.Type == "measurement_start" || event.Type == "measurement_checkpoint" || event.Type == "measurement_stop"
	if event.SessionID != "" || definition == sessionWindowDefinition || isLifecycle {
		w.enabled = true
	}
	sessionKey := measurementSessionKey{event.NodeID, event.SessionID}
	if event.SessionID != "" && event.NodeID != "" {
		session := w.sessions[sessionKey]
		if session == nil {
			session = &measurementSession{key: sessionKey}
			w.sessions[sessionKey] = session
		}
		session.sequences.add(event.Sequence)
		if event.Sequence == 0 {
			session.invalid = true
		}
		proof := sessionProof{event.Timestamp, event.Sequence}
		switch event.Type {
		case "measurement_start":
			if !session.started || event.Sequence < session.start.sequence {
				session.started, session.start = true, proof
				topics, known := targetNodeIDs(event.Fields["subscribedTopics"])
				session.known = known
				session.topics = make(map[string]struct{}, len(topics))
				for _, topic := range topics {
					session.topics[topic] = struct{}{}
				}
			}
		case "measurement_checkpoint", "measurement_stop":
			session.proofs = append(session.proofs, proof)
			if event.Type == "measurement_stop" && (session.stop == nil || event.Sequence < session.stop.sequence) {
				copyProof := proof
				session.stop = &copyProof
			}
		}
	} else if isLifecycle || definition == sessionWindowDefinition {
		w.incomplete = true
	}
	key := messageMetricKey{event.Topic, event.MessageID}
	if event.Type == "publish" && definition == sessionWindowDefinition {
		if _, exists := w.publications[key]; !exists {
			window := 10 * time.Second
			valid := !event.Timestamp.IsZero() && event.MessageID != "" && event.NodeID != ""
			if value, exists := event.Fields["deliveryWindow"]; exists {
				text, ok := value.(string)
				parsed, err := time.ParseDuration(text)
				if !ok || err != nil || parsed <= 0 || parsed > time.Hour {
					valid = false
				} else {
					window = parsed
				}
			}
			auditTargets, _ := targetNodeIDs(event.Fields["targetNodeIds"])
			w.publications[key] = windowPublication{key: key, publisher: event.NodeID, sessionID: event.SessionID,
				auditTargets: append([]string(nil), auditTargets...), at: event.Timestamp, deadline: event.Timestamp.Add(window), valid: valid}
		}
	}
	if event.MessageID == "" || event.NodeID == "" || event.Type != "deliver" && event.Type != "duplicate" {
		return
	}
	if local, _ := event.Fields["localDelivery"].(bool); local {
		return
	}
	if w.receipts[key] == nil {
		w.receipts[key] = make(map[measurementSessionKey]*windowReceipts)
	}
	receipts := w.receipts[key][sessionKey]
	if receipts == nil {
		receipts = &windowReceipts{}
		w.receipts[key][sessionKey] = receipts
	}
	proof := sessionProof{event.Timestamp, event.Sequence}
	if event.Type == "duplicate" {
		receipts.duplicates = append(receipts.duplicates, proof)
		return
	}
	encoding, _ := event.Fields["payloadEncoding"].(string)
	available, _ := event.Fields["latencyAvailable"].(bool)
	receipts.deliveries = append(receipts.deliveries, windowDelivery{proof, deliveryMetric{event.Timestamp, event.AgentID, event.LatencyMS, encoding == "envelope" && available}})
}

type evaluatedSession struct {
	source        *measurementSession
	aliveThrough  time.Time
	completeUntil time.Time
	stopComplete  bool
	end           *time.Time
}

func evaluateSession(session *measurementSession) (*evaluatedSession, bool) {
	if !session.started || !session.known || session.invalid || session.start.at.IsZero() || session.start.sequence == 0 {
		return nil, false
	}
	if session.stop != nil && (session.stop.at.Before(session.start.at) || session.stop.sequence < session.start.sequence) {
		return nil, false
	}
	result := &evaluatedSession{source: session}
	if session.stop != nil {
		end := session.stop.at
		result.end = &end
	}
	through := session.sequences.through(session.start.sequence)
	for _, proof := range session.proofs {
		if proof.sequence < session.start.sequence || proof.at.Before(session.start.at) || proof.at.IsZero() {
			return nil, false
		}
		if session.stop != nil && (proof.sequence > session.stop.sequence || proof.at.After(session.stop.at)) {
			return nil, false
		}
		if proof.at.After(result.aliveThrough) {
			result.aliveThrough = proof.at
		}
		if proof.sequence <= through && proof.at.After(result.completeUntil) {
			result.completeUntil = proof.at
		}
	}
	result.stopComplete = session.stop != nil && session.stop.sequence <= through
	return result, true
}

type sessionTopicIndex struct {
	starts, ends []*evaluatedSession
	publications []windowPublication
}

// Sorting lifecycle boundaries once and sweeping publication times avoids
// scanning every historical session for every message. The remaining work is
// proportional to actual candidate message/session pairs and their receipts.
func (w *sessionWindowAccumulator) summarize(runID string, asOf time.Time, published, delivered, duplicates int) (model.Metrics, []propagationSample) {
	result := model.Metrics{Definition: sessionWindowDefinition, RunID: runID, Published: published, Delivered: delivered, Duplicates: duplicates,
		LegacyPublications: max(0, published-len(w.publications)), MeasurementIncomplete: w.incomplete}
	topics := make(map[string]*sessionTopicIndex)
	knownStartNodes := make(map[string]struct{})
	indexFor := func(topic string) *sessionTopicIndex {
		if topics[topic] == nil {
			topics[topic] = &sessionTopicIndex{}
		}
		return topics[topic]
	}
	for _, session := range w.sessions {
		evaluated, known := evaluateSession(session)
		if !known {
			result.MeasurementIncomplete = true
			continue
		}
		knownStartNodes[session.key.nodeID] = struct{}{}
		for _, terminated := range w.terminations[session.key.nodeID] {
			if !terminated.Before(session.start.at) && (evaluated.end == nil || terminated.Before(*evaluated.end)) {
				end := terminated
				evaluated.end, evaluated.stopComplete = &end, false
			}
		}
		if evaluated.end != nil && evaluated.aliveThrough.After(*evaluated.end) {
			result.MeasurementIncomplete = true
			continue
		}
		for topic := range session.topics {
			index := indexFor(topic)
			index.starts = append(index.starts, evaluated)
			if evaluated.end != nil {
				index.ends = append(index.ends, evaluated)
			}
		}
	}
	for key, publication := range w.publications {
		if !publication.valid {
			result.MeasurementIncomplete = true
			continue
		}
		// The dispatch snapshot is only a missing-evidence audit hint. It never
		// adds a receiver to either cohort or overrides actual session timing.
		for _, nodeID := range publication.auditTargets {
			if _, known := knownStartNodes[nodeID]; nodeID != "" && !known {
				result.MeasurementIncomplete = true
			}
		}
		window := publication.deadline.Sub(publication.at).String()
		if !containsWindow(result.DeliveryWindows, window) {
			result.DeliveryWindows = append(result.DeliveryWindows, window)
		}
		for session := range w.receipts[key] {
			if session.sessionID == "" {
				result.MeasurementIncomplete = true
			}
		}
		if asOf.Before(publication.deadline) {
			result.PendingPublications++
			continue
		}
		result.FinalizedPublications++
		indexFor(key.topic).publications = append(indexFor(key.topic).publications, publication)
	}
	sort.Strings(result.DeliveryWindows)
	var samples []propagationSample
	var latencies []float64
	for _, index := range topics {
		sort.Slice(index.starts, func(i, j int) bool { return index.starts[i].source.start.at.Before(index.starts[j].source.start.at) })
		sort.Slice(index.ends, func(i, j int) bool { return index.ends[i].end.Before(*index.ends[j].end) })
		sort.Slice(index.publications, func(i, j int) bool { return index.publications[i].at.Before(index.publications[j].at) })
		active := make(map[measurementSessionKey]*evaluatedSession)
		start, end := 0, 0
		for _, publication := range index.publications {
			for start < len(index.starts) && !index.starts[start].source.start.at.After(publication.at) {
				session := index.starts[start]
				active[session.source.key] = session
				start++
			}
			for end < len(index.ends) && !index.ends[end].end.After(publication.at) {
				delete(active, index.ends[end].source.key)
				end++
			}
			for key, session := range active {
				if key.nodeID == publication.publisher {
					continue
				}
				if session.aliveThrough.Before(publication.at) {
					result.AvailabilityUnknownPairs++
					continue
				}
				result.InitialExpectedDeliveries++
				receipt := inspectWindowReceipts(w.receipts[publication.key][key], publication, session)
				departed := session.end != nil && session.end.Before(publication.deadline)
				stable := !departed && !session.aliveThrough.Before(publication.deadline)
				if receipt.ontime != nil {
					result.InitialEligibleDeliveries++
				} else if receipt.invalid || (departed && !session.stopComplete) || (!departed && session.completeUntil.Before(publication.deadline)) {
					result.InitialUnknownDeliveries++
				}
				if departed {
					result.DepartedPairs++
					continue
				}
				if !stable {
					result.AvailabilityUnknownPairs++
					continue
				}
				result.ExpectedDeliveries++
				if receipt.ontime == nil {
					if receipt.invalid || session.completeUntil.Before(publication.deadline) {
						result.UnknownDeliveries++
					} else {
						result.MissedDeliveries++
					}
					if receipt.late {
						result.LateDeliveries++
					}
					if receipt.invalid {
						result.InvalidLatencySamples++
					}
					continue
				}
				result.EligibleDeliveries++
				result.EligibleDuplicates += receipt.duplicates
				value := receipt.ontime.value
				if value.latencyAvailable {
					latencies = append(latencies, value.latencyMS)
					samples = append(samples, propagationSample{propagationSeriesKey{value.agentID, publication.key.topic}, value.latencyMS / 1000})
				}
			}
		}
	}
	result.DeliveryRatioAvailable = result.ExpectedDeliveries > 0
	if result.DeliveryRatioAvailable {
		result.Reachability = float64(result.EligibleDeliveries) / float64(result.ExpectedDeliveries)
		result.DeliveryRatioUpperBound = float64(result.EligibleDeliveries+result.UnknownDeliveries) / float64(result.ExpectedDeliveries)
	}
	result.InitialDeliveryRatioAvailable = result.InitialExpectedDeliveries > 0 && result.AvailabilityUnknownPairs == 0 && !result.MeasurementIncomplete
	result.StableCoverageAvailable = result.InitialDeliveryRatioAvailable
	if result.InitialExpectedDeliveries > 0 {
		result.InitialDeliveryRatio = float64(result.InitialEligibleDeliveries) / float64(result.InitialExpectedDeliveries)
		result.InitialDeliveryRatioUpperBound = float64(result.InitialEligibleDeliveries+result.InitialUnknownDeliveries) / float64(result.InitialExpectedDeliveries)
		result.StableCoverage = float64(result.ExpectedDeliveries) / float64(result.InitialExpectedDeliveries)
	}
	result.DuplicateSamples = result.EligibleDeliveries
	if result.DuplicateSamples > 0 {
		result.AverageDuplicates = float64(result.EligibleDuplicates) / float64(result.DuplicateSamples)
	}
	result.LatencySamples = len(latencies)
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		for _, value := range latencies {
			result.AverageLatencyMS += value
		}
		result.AverageLatencyMS /= float64(len(latencies))
		result.P95LatencyMS = latencies[int(math.Ceil(0.95*float64(len(latencies))))-1]
	}
	return result, samples
}

func containsWindow(windows []string, candidate string) bool {
	for _, window := range windows {
		if candidate == window {
			return true
		}
	}
	return false
}

type windowReceiptResult struct {
	ontime     *windowDelivery
	late       bool
	invalid    bool
	duplicates int
}

func inspectWindowReceipts(receipts *windowReceipts, publication windowPublication, evaluated *evaluatedSession) windowReceiptResult {
	var result windowReceiptResult
	if receipts == nil {
		return result
	}
	session := evaluated.source
	for i := range receipts.deliveries {
		delivery := &receipts.deliveries[i]
		at, value := delivery.proof.at, delivery.value
		if at.Before(publication.at) || at.Before(session.start.at) || delivery.proof.sequence < session.start.sequence ||
			(evaluated.end != nil && at.After(*evaluated.end)) ||
			(session.stop != nil && (at.After(session.stop.at) || delivery.proof.sequence > session.stop.sequence)) ||
			(value.latencyAvailable && (value.latencyMS < 0 || math.IsNaN(value.latencyMS) || math.IsInf(value.latencyMS, 0))) {
			result.invalid = true
			continue
		}
		if at.After(publication.deadline) {
			result.late = true
			continue
		}
		if result.ontime == nil || at.Before(result.ontime.proof.at) {
			result.ontime = delivery
		}
	}
	for _, duplicate := range receipts.duplicates {
		if !duplicate.at.Before(publication.at) && !duplicate.at.After(publication.deadline) && duplicate.sequence >= session.start.sequence &&
			(evaluated.end == nil || !duplicate.at.After(*evaluated.end)) &&
			(session.stop == nil || !duplicate.at.After(session.stop.at) && duplicate.sequence <= session.stop.sequence) {
			result.duplicates++
		}
	}
	return result
}
