package controller

import "testing"

func TestLocalPublicationHasNoRemoteDeliveryWindow(t *testing.T) {
	events := windowEvents(windowStart("receiver", "receiver-session", 0, "topic"),
		windowEvent("receiver", "receiver-session", 2, "measurement_checkpoint", 22))
	events[1].Fields["localPublication"] = true
	// Session membership is independent of dispatch hints; local-only semantics
	// must exclude even live remote subscribers, including an old cohort hint.
	events[1].Fields["targetNodeIds"] = []string{"receiver"}
	for _, now := range []int{12, 30} {
		got := windowSummary(events, now)
		if got.Published != 1 || got.ExpectedDeliveries != 0 || got.InitialExpectedDeliveries != 0 || got.MissedDeliveries != 0 || got.FinalizedPublications != 0 || got.PendingPublications != 0 {
			t.Fatalf("local publication affected remote metrics: %+v", got)
		}
	}
}
