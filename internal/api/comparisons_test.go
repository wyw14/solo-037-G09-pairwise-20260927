package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-037/internal/service"
)

func createMeasuredRunForComparison(t *testing.T, server http.Handler, name string) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"model_name":"hull A","section":"section-a","carriage":"carriage-1"}`, name)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/measured-runs", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("create run: %d %s", response.Code, response.Body)
	}
	var decoded struct {
		Data service.MeasuredRun `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	return string(decoded.Data.Operation.ID)
}

func TestComparisonGroupHTTPFlow(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()
	first := createMeasuredRunForComparison(t, server, "tow 1")
	second := createMeasuredRunForComparison(t, server, "tow 2")
	stamp := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	for index, id := range []string{first, second} {
		input := fmt.Sprintf(`{"kind":"force","source_id":"load-cell","value":%d,"recorded_at":%q}`, 10+index, stamp)
		recorded := httptest.NewRecorder()
		server.ServeHTTP(recorded, httptest.NewRequest(http.MethodPost, "/api/measured-runs/"+id+"/evidence", strings.NewReader(input)))
		if recorded.Code != http.StatusOK {
			t.Fatalf("record: %d %s", recorded.Code, recorded.Body)
		}
	}
	created := httptest.NewRecorder()
	groupBody := fmt.Sprintf(`{"name":"repeat tows","run_ids":[%q,%q]}`, first, second)
	server.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/comparison-groups", strings.NewReader(groupBody)))
	if created.Code != http.StatusOK {
		t.Fatalf("create group: %d %s", created.Code, created.Body)
	}
	var groupResponse struct {
		Data service.ComparisonGroup `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &groupResponse); err != nil {
		t.Fatal(err)
	}
	groupID := string(groupResponse.Data.ID)
	if groupID == "" {
		t.Fatal("missing group ID")
	}
	analysis := httptest.NewRecorder()
	server.ServeHTTP(analysis, httptest.NewRequest(http.MethodGet, "/api/comparison-groups/"+groupID+"/analysis", nil))
	if analysis.Code != http.StatusOK || !strings.Contains(analysis.Body.String(), `"comparable":true`) {
		t.Fatalf("analysis: %d %s", analysis.Code, analysis.Body)
	}
	if !strings.Contains(analysis.Body.String(), `"mean_spread":1`) {
		t.Fatalf("analysis should report force mean spread: %s", analysis.Body)
	}
	listed := httptest.NewRecorder()
	server.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/comparison-groups", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), groupID) {
		t.Fatalf("list: %d %s", listed.Code, listed.Body)
	}
	updated := httptest.NewRecorder()
	server.ServeHTTP(updated, httptest.NewRequest(http.MethodPut, "/api/comparison-groups/"+groupID, strings.NewReader(groupBody)))
	if updated.Code != http.StatusOK {
		t.Fatalf("update: %d %s", updated.Code, updated.Body)
	}
}

func TestComparisonGroupHTTPRejectsBadRequests(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()
	first := createMeasuredRunForComparison(t, server, "tow 1")
	unknownRun := httptest.NewRecorder()
	body := fmt.Sprintf(`{"name":"bad","run_ids":[%q,"missing"]}`, first)
	server.ServeHTTP(unknownRun, httptest.NewRequest(http.MethodPost, "/api/comparison-groups", strings.NewReader(body)))
	if unknownRun.Code != http.StatusBadRequest {
		t.Fatalf("unknown run: %d %s", unknownRun.Code, unknownRun.Body)
	}
	missing := httptest.NewRecorder()
	server.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/comparison-groups/nope/analysis", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown group analysis: %d %s", missing.Code, missing.Body)
	}
	missingUpdate := httptest.NewRecorder()
	server.ServeHTTP(missingUpdate, httptest.NewRequest(http.MethodPut, "/api/comparison-groups/nope", strings.NewReader(body)))
	if missingUpdate.Code != http.StatusNotFound {
		t.Fatalf("unknown group update: %d %s", missingUpdate.Code, missingUpdate.Body)
	}
}
