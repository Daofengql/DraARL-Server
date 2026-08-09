package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMessageAPIMetricsTrackRequestsQueriesAndErrors(t *testing.T) {
	before := GetMessageAPIMetrics()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Status(http.StatusBadRequest)
	finish := beginMessageAPIRequest(context, false)
	observeMessageQuery(false, time.Millisecond)
	messageScopeRejects.Add(1)
	messageParameterRejects.Add(1)
	messageCursorRejects.Add(1)
	messageQueryErrors.Add(1)
	messageVisibleGroups.Add(2)
	messageRowsReturned.Add(3)
	messageAudioURLs.Add(1)
	messageAudioURLFailures.Add(1)
	finish()
	after := GetMessageAPIMetrics()

	wantDelta := map[string]uint64{
		"list_requests": 1, "responses_4xx": 1, "scope_rejects": 1,
		"parameter_rejects": 1, "cursor_rejects": 1, "query_errors": 1,
		"visible_groups": 2, "rows_returned": 3, "audio_urls_authorized": 1,
		"audio_url_failures": 1,
	}
	for key, want := range wantDelta {
		if got := after[key] - before[key]; got != want {
			t.Fatalf("metric %s delta=%d want=%d before=%v after=%v", key, got, want, before, after)
		}
	}
	if after["list_query_ns"] <= before["list_query_ns"] || after["request_ns"] <= before["request_ns"] {
		t.Fatalf("duration metrics did not advance before=%v after=%v", before, after)
	}
	if after["query_le_5ms"]-before["query_le_5ms"] != 1 || after["request_le_5s"]-before["request_le_5s"] != 1 {
		t.Fatalf("latency histograms did not advance before=%v after=%v", before, after)
	}
}
