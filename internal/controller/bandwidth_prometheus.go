package controller

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type bandwidthCollector struct {
	state                                                      *state
	total, protocolTotal, rate, protocolRate, timestamp, final *prometheus.Desc
}

func newBandwidthCollector(s *state) *bandwidthCollector {
	labels := []string{"run_id", "agent_id", "node_id", "session_id"}
	direction := append(append([]string{}, labels...), "direction")
	protocols := append(append([]string{}, direction...), "protocol")
	return &bandwidthCollector{state: s,
		total:         prometheus.NewDesc("kpl_p2p_stream_bytes_total", "Cumulative libp2p stream bytes per process session; excludes transport overhead and HTTP management traffic.", direction, nil),
		protocolTotal: prometheus.NewDesc("kpl_p2p_protocol_stream_bytes_total", "Cumulative libp2p stream bytes attributed to the negotiated protocol (empty during negotiation).", protocols, nil),
		rate:          prometheus.NewDesc("kpl_p2p_stream_bits_per_second", "Last reported interval-average libp2p stream throughput using source monotonic time; absent after 15 seconds without a non-final sample.", direction, nil),
		protocolRate:  prometheus.NewDesc("kpl_p2p_protocol_stream_bits_per_second", "Last reported interval-average protocol stream throughput using source monotonic time.", protocols, nil),
		timestamp:     prometheus.NewDesc("kpl_p2p_bandwidth_sample_timestamp_seconds", "Source timestamp of the latest accepted bandwidth sample.", labels, nil),
		final:         prometheus.NewDesc("kpl_p2p_bandwidth_session_final", "One if the last sample was taken after normal host closure; zero otherwise.", labels, nil),
	}
}
func (c *bandwidthCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.total, c.protocolTotal, c.rate, c.protocolRate, c.timestamp, c.final} {
		ch <- d
	}
}
func (c *bandwidthCollector) Collect(ch chan<- prometheus.Metric) {
	type row struct {
		run     string
		key     bandwidthSessionKey
		session bandwidthSession
	}
	rows := []row{}
	c.state.mu.RLock()
	for run, a := range c.state.runMetrics {
		for key, session := range a.bandwidth.sessions {
			rows = append(rows, row{run, key, session})
		}
	}
	c.state.mu.RUnlock()
	now := time.Now()
	for _, r := range rows {
		s := r.session
		labels := []string{r.run, s.agentID, r.key.nodeID, r.key.sessionID}
		ch <- prometheus.MustNewConstMetric(c.timestamp, prometheus.GaugeValue, float64(s.at.Unix())+float64(s.at.Nanosecond())/1e9, labels...)
		final := 0.
		if s.sample.Final {
			final = 1
		}
		ch <- prometheus.MustNewConstMetric(c.final, prometheus.GaugeValue, final, labels...)
		fresh := s.sample.Final || (now.Sub(s.at) >= -15*time.Second && now.Sub(s.at) <= 15*time.Second)
		factor := 8e9 / float64(s.interval.durationNS)
		if s.sample.Final {
			factor = 0
		} // A closed session consumes no current bandwidth.
		for _, sent := range []bool{false, true} {
			direction := "receive"
			total, delta := s.sample.ReceivedBytes, s.interval.receivedBytes
			if sent {
				direction = "send"
				total, delta = s.sample.SentBytes, s.interval.sentBytes
			}
			values := append(append([]string{}, labels...), direction)
			ch <- prometheus.MustNewConstMetric(c.total, prometheus.CounterValue, float64(total), values...)
			if fresh {
				ch <- prometheus.MustNewConstMetric(c.rate, prometheus.GaugeValue, float64(delta)*factor, values...)
			}
			for _, p := range s.sample.Protocols {
				bytes := p.ReceivedBytes
				if sent {
					bytes = p.SentBytes
				}
				ch <- prometheus.MustNewConstMetric(c.protocolTotal, prometheus.CounterValue, float64(bytes), append(values, p.Protocol)...)
			}
			if fresh {
				for _, p := range s.interval.protocols {
					bytes := p.ReceivedBytes
					if sent {
						bytes = p.SentBytes
					}
					ch <- prometheus.MustNewConstMetric(c.protocolRate, prometheus.GaugeValue, float64(bytes)*factor, append(values, p.Protocol)...)
				}
			}
		}
	}
}
