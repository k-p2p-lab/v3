package peer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/k-p2p-lab/v3/internal/model"
)

func TestPrepareNetworkCannotModifyProcessNamespace(t *testing.T) {
	config := model.PeerProcessConfig{Runtime: "process", NodeConfig: model.NodeConfig{Network: model.NetworkConfig{Delay: "100ms"}}}
	if err := prepareNetwork(context.Background(), config); err == nil || !strings.Contains(err.Error(), "isolated Docker peer") {
		t.Fatalf("process network preparation = %v", err)
	}
	config.NodeConfig.Network.Delay = "0s"
	if err := prepareNetwork(context.Background(), config); err != nil {
		t.Fatalf("disabled network should not invoke tc: %v", err)
	}
}

func TestPublishRejectsRecycledContainerAddress(t *testing.T) {
	s := &Server{config: model.PeerProcessConfig{Node: model.Node{ID: "new-peer"}}}
	request := httptest.NewRequest(http.MethodPost, "/publish", strings.NewReader(`{"runId":"run"}`))
	request.Header.Set("X-KPL-Node-ID", "old-peer")
	response := httptest.NewRecorder()
	s.handlePublish(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d; recycled IP must not accept old peer's publish", response.Code)
	}
}
