package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChurnAPIContracts(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing bearer token for %s %s", r.Method, r.URL.Path)
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/devices/11/group":
			if r.Method != http.MethodPut {
				t.Errorf("group method=%s", r.Method)
			}
			assertJSONNumber(t, r, "group_id", 22)
		case "/api/groups/22/devices/11/comm-control":
			if r.Method != http.MethodPut {
				t.Errorf("comm method=%s", r.Method)
			}
		case "/api/admin/devices/11/config":
			if r.Method != http.MethodPut {
				t.Errorf("config method=%s", r.Method)
			}
		case "/api/admin/devices/11/config/sync":
			if r.Method != http.MethodPost {
				t.Errorf("sync method=%s", r.Method)
			}
		case "/api/edge-nodes":
			_, _ = w.Write([]byte(`{"code":200,"data":{"items":[{"id":7,"node_id":"edge-a","runtime":{"online":true}}]}}`))
			return
		case "/api/edge-nodes/7/disconnect":
			if r.Method != http.MethodPost {
				t.Errorf("disconnect method=%s", r.Method)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
	}))
	defer server.Close()

	api, err := newChurnAPI(server.URL+"/api/", strings.Repeat("s", 32), usernameForUser(0))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := api.changeGroup(ctx, 11, 22); err != nil {
		t.Fatal(err)
	}
	if err := api.setCommControl(ctx, 22, 11, true, false); err != nil {
		t.Fatal(err)
	}
	if err := api.syncConfig(ctx, 11, 77); err != nil {
		t.Fatal(err)
	}
	nodes, err := api.edgeNodes(ctx)
	if err != nil || len(nodes) != 1 || nodes[0].ID != 7 || !nodes[0].Runtime.Online {
		t.Fatalf("nodes=%+v err=%v", nodes, err)
	}
	if err := api.resetEdge(ctx, nodes[0]); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 6 {
		t.Fatalf("requests=%d want=6", got)
	}
}

func TestWaitForCounter(t *testing.T) {
	var value atomic.Uint64
	go func() {
		time.Sleep(10 * time.Millisecond)
		value.Add(1)
	}()
	if err := waitForCounter(t.Context(), value.Load, 0, time.Second); err != nil {
		t.Fatal(err)
	}
}

func assertJSONNumber(t *testing.T, r *http.Request, key string, want float64) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body[key] != want {
		t.Errorf("%s=%v want=%v", key, body[key], want)
	}
}
