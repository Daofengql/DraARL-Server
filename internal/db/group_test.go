package db

import (
	"strings"
	"testing"
)

func TestRelayListQueryUsesStableColumnList(t *testing.T) {
	query := strings.Join(strings.Fields(relayListQuery), " ")
	want := "SELECT id, name, up_freq, down_freq, send_ctss, recive_ctss, ower_callsign, create_time, update_time, status, note FROM relay WHERE status = 1 ORDER BY id"
	if query != want {
		t.Fatalf("query=%q want=%q", query, want)
	}
	if strings.Contains(strings.ToUpper(relayListQuery), "SELECT *") {
		t.Fatalf("relay list query must not use SELECT *: %s", relayListQuery)
	}
}

func TestServerListQueryUsesOnlyStableProjection(t *testing.T) {
	query := strings.Join(strings.Fields(serverListQuery), " ")
	want := "SELECT id, name, dns_name, is_online, create_time, update_time FROM servers WHERE status = 1 ORDER BY id"
	if query != want {
		t.Fatalf("query=%q want=%q", query, want)
	}
	if strings.Contains(strings.ToUpper(serverListQuery), "SELECT *") {
		t.Fatalf("server list query must not use SELECT *: %s", serverListQuery)
	}
}
