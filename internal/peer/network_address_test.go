package peer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

func TestDockerP2PUsesAgentRouteAndPreservesExplicitListen(t *testing.T) {
	for _, test := range []struct {
		name, runtime, listen, localIP, want, wantError string
	}{
		{"overlay wildcard", "docker", "/ip4/0.0.0.0/tcp/20000", "10.1.0.7", "/ip4/10.1.0.7/tcp/20000", ""},
		{"explicit overlay", "docker", "/ip4/10.1.0.7/tcp/20000", "10.1.0.7", "/ip4/10.1.0.7/tcp/20000", ""},
		{"custom port", "docker", "/ip4/0.0.0.0/tcp/21000", "10.1.0.7", "/ip4/10.1.0.7/tcp/21000", ""},
		{"wrong bridge", "docker", "/ip4/172.18.0.7/tcp/20000", "10.1.0.7", "", "must match"},
		{"loopback route", "docker", "/ip4/0.0.0.0/tcp/20000", "127.0.0.1", "", "non-loopback IPv4 route"},
		{"no IPv4 route", "docker", "/ip4/0.0.0.0/tcp/20000", "fd00::7", "", "non-loopback IPv4 route"},
		{"explicit loopback", "docker", "/ip4/127.0.0.1/tcp/20000", "10.1.0.7", "", "non-loopback"},
		{"IPv6 listen", "docker", "/ip6/::/tcp/20000", "10.1.0.7", "", "IPv4 TCP"},
		{"UDP listen", "docker", "/ip4/0.0.0.0/udp/20000", "10.1.0.7", "", "IPv4 TCP"},
		{"ephemeral port", "docker", "/ip4/0.0.0.0/tcp/0", "10.1.0.7", "", "port between"},
		{"process wildcard", "process", "/ip4/0.0.0.0/tcp/20000", "", "/ip4/0.0.0.0/tcp/20000", ""},
		{"process IPv6", "process", "/ip6/::1/tcp/0", "", "/ip6/::1/tcp/0", ""},
		{"legacy process", "", "/ip4/127.0.0.1/tcp/0", "", "/ip4/127.0.0.1/tcp/0", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := model.PeerProcessConfig{Runtime: test.runtime, P2PListen: test.listen, AgentURL: "http://agent-task:8090"}
			routeCalls := 0
			resolved, err := resolveDockerP2PListen(context.Background(), config, func(ctx context.Context, endpoint string) (net.IP, error) {
				routeCalls++
				if endpoint != config.AgentURL {
					t.Fatalf("route target = %s", endpoint)
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("route discovery must have a bounded timeout")
				}
				return net.ParseIP(test.localIP), nil
			})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want %s", err, test.wantError)
				}
				return
			}
			if err != nil || resolved.P2PListen != test.want {
				t.Fatalf("listen = %s, error = %v", resolved.P2PListen, err)
			}
			if test.runtime != "docker" && routeCalls != 0 {
				t.Fatal("process mode unexpectedly performed Docker route discovery")
			}
			if config.P2PListen != test.listen {
				t.Fatal("original config was modified")
			}
		})
	}
}

func TestDockerAddressDiscoveryPrecedesWholeInterfaceLoss(t *testing.T) {
	config := model.PeerProcessConfig{Runtime: "docker", AgentURL: "http://agent:8090", P2PListen: "/ip4/0.0.0.0/tcp/20000",
		NodeConfig: model.NodeConfig{Network: model.NetworkConfig{Scope: "all", LossPercent: func() *float64 { value := 100.0; return &value }()}}}
	var calls []string
	resolved, err := preparePeerNetwork(context.Background(), config,
		func(context.Context, string) (net.IP, error) {
			calls = append(calls, "route")
			return net.ParseIP("10.1.0.7"), nil
		}, func(_ context.Context, config model.PeerProcessConfig) error {
			calls = append(calls, "netem")
			if config.P2PListen != "/ip4/10.1.0.7/tcp/20000" {
				t.Fatalf("netem ran before concrete address selection: %+v", config)
			}
			return nil
		})
	if err != nil || resolved.P2PListen != "/ip4/10.1.0.7/tcp/20000" || !reflect.DeepEqual(calls, []string{"route", "netem"}) {
		t.Fatalf("resolved = %+v, calls = %v, error = %v", resolved, calls, err)
	}
	calls = nil
	_, err = preparePeerNetwork(context.Background(), config,
		func(context.Context, string) (net.IP, error) { return nil, errors.New("DNS lookup failed") },
		func(context.Context, model.PeerProcessConfig) error { calls = append(calls, "netem"); return nil })
	if err == nil || len(calls) != 0 {
		t.Fatalf("failed route discovery must not mutate network: %v, %v", err, calls)
	}
}

func TestAgentRouteLookupDoesNotSendUDPData(t *testing.T) {
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ip, err := agentRouteSourceIPv4(context.Background(), "http://"+listener.LocalAddr().String())
	if err != nil || !ip.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("route source = %s, error = %v", ip, err)
	}
	_ = listener.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := listener.ReadFrom(make([]byte, 1)); err == nil {
		t.Fatal("route discovery sent a UDP payload")
	} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := agentRouteSourceIPv4(ctx, "http://"+listener.LocalAddr().String()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled route lookup = %v", err)
	}
	for _, endpoint := range []string{"agent:8090", "ftp://agent", "http://[::1]:8090"} {
		if _, err := agentRouteSourceIPv4(context.Background(), endpoint); err == nil {
			t.Fatalf("unsupported Agent endpoint accepted: %s", endpoint)
		}
	}
}

func TestDockerHostAndStatusAdvertiseOnlySelectedAddress(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	var localIP net.IP
	for _, device := range interfaces {
		if device.Flags&net.FlagUp == 0 || device.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := device.Addrs()
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil && network.IP.IsGlobalUnicast() && !network.IP.IsLoopback() {
				localIP = network.IP
				break
			}
		}
		if localIP != nil {
			break
		}
	}
	if localIP == nil {
		t.Skip("no non-loopback IPv4 interface for real libp2p listen verification")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(localIP.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	want := fmt.Sprintf("/ip4/%s/tcp/%d", localIP, port)
	reports := make(chan model.Node, 1)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var node model.Node
		if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
			t.Error(err)
		}
		reports <- node
		w.WriteHeader(http.StatusNoContent)
	}))
	defer agent.Close()
	noDHT := false
	config := model.PeerProcessConfig{Runtime: "docker", P2PListen: want, AgentURL: agent.URL, ControllerURL: "http://controller:8080",
		Node:       model.Node{ID: "test-peer", RunID: "test-run", Addresses: []string{"/ip4/172.18.0.9/tcp/20000"}},
		NodeConfig: model.NodeConfig{Kademlia: model.KademliaConfig{Enabled: &noDHT}}}
	// Construct the server after route discovery, as Run does before netem.
	server, err := newServer(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if addresses := server.host.Addrs(); len(addresses) != 1 || addresses[0].String() != want {
		t.Fatalf("host advertises unrelated addresses: %v", addresses)
	}
	var listenAddresses []string
	for _, address := range server.host.Network().ListenAddresses() {
		// Circuit relay registers a virtual listener, not another IP socket.
		if address.String() != "/p2p-circuit" {
			listenAddresses = append(listenAddresses, address.String())
		}
	}
	if !reflect.DeepEqual(listenAddresses, []string{want}) {
		t.Fatalf("host listens on unrelated IP addresses: %v", listenAddresses)
	}
	if err := server.reportStatus(context.Background(), model.NodeReady, ""); err != nil {
		t.Fatal(err)
	}
	if report := <-reports; !reflect.DeepEqual(report.Addresses, []string{want}) {
		t.Fatalf("status includes stale or unrelated addresses: %v", report.Addresses)
	}
}
