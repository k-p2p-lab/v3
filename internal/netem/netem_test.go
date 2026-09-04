package netem

import (
	"context"
	"errors"
	"net"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/k-p2p-lab/v3/internal/model"
)

func floatPtr(value float64) *float64 { return &value }
func intPtr(value int) *int           { return &value }

func ipv4Interface(name string) networkInterface {
	return networkInterface{name: name, flags: net.FlagUp, addresses: []net.Addr{&net.IPNet{IP: net.ParseIP("172.20.0.2"), Mask: net.CIDRMask(16, 32)}}}
}

func TestApplyShapesOnlyP2PTrafficEvenWithTotalLoss(t *testing.T) {
	var calls [][]string
	err := apply(context.Background(), model.NetworkConfig{LossPercent: floatPtr(100)}, 0, []networkInterface{ipv4Interface("eth0")}, func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("got %d commands, want root, netem, and 2 selective filters", len(calls))
	}
	root := strings.Join(calls[0], " ")
	if !strings.Contains(root, "root handle 1: prio bands 3 priomap ") {
		t.Fatalf("missing classful qdisc: %s", root)
	}
	priomap := calls[0][11:]
	if len(priomap) != 16 {
		t.Fatalf("unexpected priority map: %v", priomap)
	}
	for _, band := range priomap {
		if band != "0" {
			t.Fatalf("unclassified control traffic enters an impaired band: %v", priomap)
		}
	}
	if actual := strings.Join(calls[1], " "); actual != "qdisc replace dev eth0 parent 1:3 handle 30: netem loss 100%" {
		t.Fatalf("loss was not confined to P2P class: %s", actual)
	}
	// Evaluate the generated protocol/port predicates against representative
	// traffic, including both connection directions and unrelated UDP ports.
	for _, packet := range []struct {
		name                          string
		protocol, source, destination int
		impaired                      bool
	}{
		{"outbound P2P dial", 6, 41000, 20000, true},
		{"accepted P2P connection reply", 6, 20000, 41000, true},
		{"peer control request", 6, 41000, 8080, false},
		{"peer control response", 6, 8080, 41000, false},
		{"agent telemetry request", 6, 41001, 8091, false},
		{"agent telemetry response", 6, 8091, 41001, false},
		{"UDP same port", 17, 20000, 20000, false},
	} {
		t.Run(packet.name, func(t *testing.T) {
			impaired := false
			for _, filter := range calls[2:] {
				if matchesPortFilter(t, filter, packet.protocol, packet.source, packet.destination) {
					impaired = true
				}
			}
			if impaired != packet.impaired {
				t.Fatalf("impaired=%v, want %v", impaired, packet.impaired)
			}
		})
	}
}

func matchesPortFilter(t *testing.T, args []string, protocol, source, destination int) bool {
	t.Helper()
	if args[0] != "filter" || args[4] != "protocol" || args[5] != "ip" || args[len(args)-2] != "flowid" || args[len(args)-1] != "1:3" {
		t.Fatalf("unexpected classifier: %v", args)
	}
	matched := true
	predicates := 0
	for i := 0; i+4 < len(args); i++ {
		if args[i] != "match" {
			continue
		}
		if args[i+1] != "ip" {
			t.Fatalf("unexpected filter protocol: %v", args)
		}
		value, err := strconv.Atoi(args[i+3])
		if err != nil {
			t.Fatal(err)
		}
		switch args[i+2] {
		case "protocol":
			if args[i+4] != "0xff" {
				t.Fatal("protocol match is not exact")
			}
			matched = matched && protocol == value
		case "sport":
			if args[i+4] != "0xffff" {
				t.Fatal("source port match is not exact")
			}
			matched = matched && source == value
		case "dport":
			if args[i+4] != "0xffff" {
				t.Fatal("destination port match is not exact")
			}
			matched = matched && destination == value
		default:
			t.Fatalf("unknown predicate: %v", args)
		}
		predicates++
	}
	if predicates != 2 {
		t.Fatalf("expected exact protocol plus port predicates: %v", args)
	}
	return matched
}

func TestApplyMapsEveryImpairmentAndDurationUnits(t *testing.T) {
	config := model.NetworkConfig{Delay: "1m2s", Jitter: "1.5ms", LossPercent: floatPtr(1.25), DuplicatePercent: floatPtr(2), CorruptPercent: floatPtr(0.1), ReorderPercent: floatPtr(4), RateMbps: floatPtr(12.5), QueueLimit: intPtr(2000)}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{"delay", "62000000000ns", "1500000ns", "distribution", "normal", "loss", "1.25%", "duplicate", "2%", "corrupt", "0.1%", "reorder", "4%", "rate", "12.5mbit", "limit", "2000"}
	if actual := options(config); !reflect.DeepEqual(actual, want) {
		t.Fatalf("tc options = %v, want %v", actual, want)
	}
}

func TestApplyVisitsActiveIPv4InterfacesOnly(t *testing.T) {
	loop := ipv4Interface("lo")
	loop.flags |= net.FlagLoopback
	down := ipv4Interface("down0")
	down.flags = 0
	v6 := networkInterface{name: "v6", flags: net.FlagUp, addresses: []net.Addr{&net.IPAddr{IP: net.ParseIP("fd00::1")}}}
	interfaces := []networkInterface{ipv4Interface("eth1"), loop, down, v6, ipv4Interface("eth0")}
	var roots []string
	err := apply(context.Background(), model.NetworkConfig{Delay: "10ms"}, 20000, interfaces, func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "qdisc" && args[4] == "root" {
			roots = append(roots, args[3])
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roots, []string{"eth0", "eth1"}) {
		t.Fatalf("interfaces = %v", roots)
	}
}

func TestApplyRejectsInvalidInputsWithoutExecutingTC(t *testing.T) {
	for _, test := range []struct {
		name       string
		config     model.NetworkConfig
		port       int
		interfaces []networkInterface
	}{
		{"invalid config", model.NetworkConfig{LossPercent: floatPtr(101)}, 20000, []networkInterface{ipv4Interface("eth0")}},
		{"bad duration", model.NetworkConfig{Delay: "1ms; echo injected"}, 20000, []networkInterface{ipv4Interface("eth0")}},
		{"bad port", model.NetworkConfig{Delay: "1ms"}, 65536, []networkInterface{ipv4Interface("eth0")}},
		{"no interface", model.NetworkConfig{Delay: "1ms"}, 20000, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := apply(context.Background(), test.config, test.port, test.interfaces, func(context.Context, ...string) ([]byte, error) { calls++; return nil, nil })
			if err == nil || calls != 0 {
				t.Fatalf("err=%v, executed %d commands", err, calls)
			}
		})
	}
}

func TestApplyRollsBackAllInstalledInterfacesOnFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls [][]string
	err := apply(ctx, model.NetworkConfig{Delay: "10ms"}, 20000, []networkInterface{ipv4Interface("eth0"), ipv4Interface("eth1")}, func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(calls) == 6 {
			cancel()
			return []byte("Specified qdisc kind is unknown"), errors.New("exit status 2")
		}
		if args[1] == "del" && ctx.Err() != nil {
			t.Fatal("rollback used canceled startup context")
		}
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "Specified qdisc kind is unknown") || !strings.Contains(err.Error(), "CAP_NET_ADMIN") {
		t.Fatalf("error should describe kernel/capability failure: %v", err)
	}
	wantCleanup := [][]string{{"qdisc", "del", "dev", "eth1", "root", "handle", "1:"}, {"qdisc", "del", "dev", "eth0", "root", "handle", "1:"}}
	if len(calls) != 8 || !reflect.DeepEqual(calls[6:], wantCleanup) {
		t.Fatalf("partial setup was not rolled back: %v", calls)
	}
}

func TestApplyReportsRollbackFailure(t *testing.T) {
	calls := 0
	err := apply(context.Background(), model.NetworkConfig{LossPercent: floatPtr(2)}, 20000, []networkInterface{ipv4Interface("eth0")}, func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if calls > 1 {
			return []byte("operation not permitted"), errors.New("exit status 2")
		}
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "remove partial network impairment on eth0") {
		t.Fatalf("rollback failure was lost: %v", err)
	}
}

func TestApplyDisabledAndUnsupportedPlatform(t *testing.T) {
	if err := Apply(context.Background(), model.NetworkConfig{Delay: "0s", LossPercent: floatPtr(0), RateMbps: floatPtr(0)}, 0); err != nil {
		t.Fatalf("disabled config should need no tc or Linux: %v", err)
	}
	if runtime.GOOS != "linux" {
		err := Apply(context.Background(), model.NetworkConfig{LossPercent: floatPtr(1)}, 20000)
		if err == nil || !strings.Contains(err.Error(), "isolated Linux peer container") {
			t.Fatalf("error=%v", err)
		}
	}
}
