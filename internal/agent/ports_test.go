package agent

import "testing"

func TestPortPoolReusesReleasedPair(t *testing.T) {
	var pool portPool
	first, ok := pool.acquire(65534, 65535)
	if !ok {
		t.Fatal("first one-slot lease was not allocated")
	}
	if _, ok := pool.acquire(65534, 65535); ok {
		t.Fatal("exhausted one-slot pool allocated a second lease")
	}
	if !pool.release(first) {
		t.Fatal("active lease was not released")
	}
	if pool.release(first) {
		t.Fatal("lease was released twice")
	}
	reused, ok := pool.acquire(65534, 65535)
	if !ok {
		t.Fatal("released one-slot lease was not reusable")
	}
	if reused.apiPort != first.apiPort || reused.p2pPort != first.p2pPort {
		t.Fatalf("reused ports = %d/%d, want %d/%d", reused.apiPort, reused.p2pPort, first.apiPort, first.p2pPort)
	}
}

func TestPortPoolAvoidsCrossRangeCollisions(t *testing.T) {
	var pool portPool
	first, ok := pool.acquire(10000, 10001)
	if !ok {
		t.Fatal("first lease was not allocated")
	}
	second, ok := pool.acquire(10000, 10001)
	if !ok {
		t.Fatal("second non-overlapping lease was not allocated")
	}
	ports := map[int]string{}
	for name, port := range map[string]int{
		"first API": first.apiPort, "first P2P": first.p2pPort,
		"second API": second.apiPort, "second P2P": second.p2pPort,
	} {
		if owner, exists := ports[port]; exists {
			t.Fatalf("port %d was assigned to both %s and %s", port, owner, name)
		}
		ports[port] = name
	}
}

func TestPortPoolRejectsInvalidOrIdenticalRanges(t *testing.T) {
	for _, bases := range [][2]int{{0, 20000}, {18000, 65536}, {18000, 18000}} {
		var pool portPool
		if lease, ok := pool.acquire(bases[0], bases[1]); ok {
			t.Fatalf("bases %v unexpectedly produced lease %+v", bases, lease)
		}
	}
}
