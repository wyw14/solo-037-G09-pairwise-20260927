package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wyw14/solo-037/internal/service"
)

func createRunForGroup(t *testing.T, server http.Handler, name, body string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/measured-runs", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("create run %s: %d %s", name, rec.Code, rec.Body)
	}
	var response struct {
		Data service.MeasuredRun `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return string(response.Data.Operation.ID)
}

func TestTrialGroupHTTPCreateListAndConsistency(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()
	runBody := func(name string) string {
		return `{"name":"` + name + `","model_name":"hull A","section":"section-a","carriage":"carriage-1","tow_speed":1.2,"wave_condition":"calm","loading":"draft-0.3"}`
	}
	a := createRunForGroup(t, server, "A", runBody("tow A"))
	b := createRunForGroup(t, server, "B", runBody("tow B"))

	created := httptest.NewRecorder()
	server.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/trial-groups",
		strings.NewReader(`{"name":"repeat set","run_ids":["`+a+`","`+b+`"]}`)))
	if created.Code != http.StatusOK {
		t.Fatalf("create group: %d %s", created.Code, created.Body)
	}
	var groupResponse struct {
		Data service.TrialGroup `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &groupResponse); err != nil {
		t.Fatal(err)
	}
	groupID := string(groupResponse.Data.ID)
	if groupID == "" || len(groupResponse.Data.RunIDs) != 2 {
		t.Fatalf("group response wrong: %s", created.Body)
	}

	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/trial-groups", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), groupID) {
		t.Fatalf("list groups: %d %s", list.Code, list.Body)
	}
	single := httptest.NewRecorder()
	server.ServeHTTP(single, httptest.NewRequest(http.MethodGet, "/api/trial-groups/"+groupID, nil))
	if single.Code != http.StatusOK {
		t.Fatalf("get group: %d", single.Code)
	}

	consistency := httptest.NewRecorder()
	server.ServeHTTP(consistency, httptest.NewRequest(http.MethodGet, "/api/trial-groups/"+groupID+"/consistency", nil))
	if consistency.Code != http.StatusOK || !strings.Contains(consistency.Body.String(), `"comparable":true`) {
		t.Fatalf("consistency: %d %s", consistency.Code, consistency.Body)
	}
}

func TestTrialGroupHTTPRejectsBadInput(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()
	body := `{"name":"tow","model_name":"hull","section":"s","carriage":"c","tow_speed":1.2,"wave_condition":"calm","loading":"d"}`
	existing := createRunForGroup(t, server, "tow", body)

	cases := map[string]string{
		"blank name":    `{"name":"  ","run_ids":["` + existing + `","x"]}`,
		"one member":    `{"name":"g","run_ids":["` + existing + `"]}`,
		"unknown run":   `{"name":"g","run_ids":["` + existing + `","00000000-0000-0000-0000-000000000000"]}`,
		"unknown field": `{"name":"g","run_ids":["a","b"],"extra":1}`,
	}
	for name, payload := range cases {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trial-groups", strings.NewReader(payload)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: got %d want 400 (%s)", name, rec.Code, rec.Body)
		}
	}

	missing := httptest.NewRecorder()
	server.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/trial-groups/00000000-0000-0000-0000-000000000000", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing group: got %d want 404", missing.Code)
	}
}

func TestTrialGroupHTTPConsistencyShowsMismatchAsBodyNotError(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()
	a := createRunForGroup(t, server, "A",
		`{"name":"A","model_name":"hull","section":"s","carriage":"c","tow_speed":1.2,"wave_condition":"calm","loading":"d"}`)
	b := createRunForGroup(t, server, "B",
		`{"name":"B","model_name":"hull","section":"s","carriage":"c","tow_speed":1.5,"wave_condition":"calm","loading":"d"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trial-groups",
		strings.NewReader(`{"name":"g","run_ids":["`+a+`","`+b+`"]}`)))
	var created struct {
		Data service.TrialGroup `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	report := httptest.NewRecorder()
	server.ServeHTTP(report, httptest.NewRequest(http.MethodGet, "/api/trial-groups/"+string(created.Data.ID)+"/consistency", nil))
	if report.Code != http.StatusOK {
		t.Fatalf("comparable=false is still 200: got %d", report.Code)
	}
	if !strings.Contains(report.Body.String(), `"comparable":false`) ||
		!strings.Contains(report.Body.String(), "condition_mismatch") ||
		!strings.Contains(report.Body.String(), "tow_speed") {
		t.Fatalf("report must name the mismatch: %s", report.Body)
	}
}
