package agones

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
)

func TestAllocatorReadsFleetCapacityAndCreatesAllocation(t *testing.T) {
	var allocationRequest allocationResource
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/fleets"):
			if request.URL.Query().Get("labelSelector") != "matchmind.dev/game=matchmind" {
				t.Errorf("label selector = %q", request.URL.Query().Get("labelSelector"))
			}
			_, _ = response.Write([]byte(`{"items":[
                    {"metadata":{"labels":{"matchmind.dev/region":"tokyo"}},"status":{"readyReplicas":2}},
                    {"metadata":{"labels":{"matchmind.dev/region":"tokyo"}},"status":{"readyReplicas":1}},
                    {"metadata":{"labels":{"matchmind.dev/region":"hongkong"}},"status":{"readyReplicas":4}}
                ]}`))
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/gameserverallocations"):
			if err := json.NewDecoder(request.Body).Decode(&allocationRequest); err != nil {
				t.Error(err)
			}
			_, _ = response.Write([]byte(`{"status":{"state":"Allocated","address":"2001:db8::1","ports":[{"name":"metrics","port":9090},{"name":"default","port":7000}]}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	allocator, err := NewAllocator(Config{
		APIURL: server.URL, Namespace: "games", BearerToken: "test-token",
		GameLabelKey: "matchmind.dev/game", GameLabelValue: "matchmind",
		RegionLabelKey: "matchmind.dev/region", HTTPClient: server.Client(),
		TokenGenerator: func() (string, error) { return "connection-token", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	capacities, err := allocator.Capacities(context.Background())
	if err != nil || capacityFor(capacities, "tokyo") != 3 || capacityFor(capacities, "hongkong") != 4 {
		t.Fatalf("capacities = %+v, %v", capacities, err)
	}
	allocation, err := allocator.Allocate(context.Background(), "match-1", "tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Address != "[2001:db8::1]:7000" || allocation.Token != "connection-token" {
		t.Fatalf("allocation = %+v", allocation)
	}
	selector := allocationRequest.Spec.Selectors[0]
	if selector.MatchLabels["matchmind.dev/region"] != "tokyo" || selector.GameServerState != "Ready" {
		t.Fatalf("selector = %+v", selector)
	}
	if allocationRequest.Spec.Metadata.Annotations["matchmind.dev/match-id"] != "match-1" ||
		allocationRequest.Spec.Metadata.Annotations["matchmind.dev/connection-token"] != "connection-token" {
		t.Fatalf("allocation annotations = %+v", allocationRequest.Spec.Metadata.Annotations)
	}
}

func TestAllocatorRejectsUnallocatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte(`{"status":{"state":"UnAllocated"}}`))
	}))
	defer server.Close()
	allocator, err := NewAllocator(Config{
		APIURL: server.URL, Namespace: "games", GameLabelKey: "game", GameLabelValue: "matchmind",
		RegionLabelKey: "region", HTTPClient: server.Client(), TokenGenerator: func() (string, error) { return "token", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Allocate(context.Background(), "match-1", "tokyo"); err != ErrAllocationRejected {
		t.Fatalf("Allocate() error = %v", err)
	}
}

func TestLoadBearerToken(t *testing.T) {
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte(" file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if token, err := LoadBearerToken("", path); err != nil || token != "file-token" {
		t.Fatalf("LoadBearerToken() = %q, %v", token, err)
	}
	if token, err := LoadBearerToken("direct-token", "missing"); err != nil || token != "direct-token" {
		t.Fatalf("LoadBearerToken(direct) = %q, %v", token, err)
	}
}

func capacityFor(capacities []application.RegionCapacity, region string) int {
	for _, capacity := range capacities {
		if capacity.Region == region {
			return capacity.AvailableServers
		}
	}
	return 0
}
