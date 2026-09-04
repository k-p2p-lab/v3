package agent

import (
	"context"
	"testing"
	"time"
)

func TestScheduleLifetimeStopTreatsZeroAsImmediate(t *testing.T) {
	stopped := make(chan struct{})
	if scheduled := scheduleLifetimeStop(context.Background(), "0s", func() { close(stopped) }); !scheduled {
		t.Fatal("explicit zero lifetime was not scheduled")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("explicit zero lifetime did not stop immediately")
	}
}

func TestScheduleLifetimeStopDistinguishesOmittedAndInvalidLifetime(t *testing.T) {
	for _, value := range []string{"", "-1s", "not-a-duration"} {
		called := make(chan struct{}, 1)
		if scheduled := scheduleLifetimeStop(context.Background(), value, func() { called <- struct{}{} }); scheduled {
			t.Errorf("lifetime %q was unexpectedly scheduled", value)
		}
		select {
		case <-called:
			t.Errorf("lifetime %q unexpectedly invoked stop", value)
		default:
		}
	}
}

func TestScheduleLifetimeStopHonorsProcessCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan struct{}, 1)
	if scheduled := scheduleLifetimeStop(ctx, "1h", func() { called <- struct{}{} }); !scheduled {
		t.Fatal("positive lifetime was not scheduled")
	}
	cancel()
	select {
	case <-called:
		t.Fatal("canceled process invoked lifetime stop")
	case <-time.After(20 * time.Millisecond):
	}
}
