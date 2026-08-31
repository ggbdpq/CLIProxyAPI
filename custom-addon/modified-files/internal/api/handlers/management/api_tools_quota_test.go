package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	datarecords "github.com/router-for-me/CLIProxyAPI/v7/custom-addon/backend"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// codexQuotaSyncFixture prepares a handler backed by a temp data-records SQLite
// database seeded with two records, plus the auth index of a codex credential
// whose email matches the "target" record. Skips when no node runtime with the
// built-in sqlite module is available.
func codexQuotaSyncFixture(t *testing.T) (string, *Handler) {
	t.Helper()
	if !datarecords.SQLiteAvailable() {
		t.Skip("node runtime with built-in sqlite module is not available")
	}
	t.Setenv("MANAGEMENT_PASSWORD", "")

	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "codex-quota-sync-auth",
		FileName: "codex-target.json",
		Provider: "codex",
		Metadata: map[string]any{
			"type":         "codex",
			"email":        "target@example.test",
			"access_token": "token-value",
		},
	}
	authIndex := auth.EnsureIndex()
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	h := NewHandler(&config.Config{}, filepath.Join(t.TempDir(), "config.yaml"), manager)
	body := "{\"email\":\"target@example.test\",\"nextTime\":\"8-8\"}\n{\"email\":\"other@example.test\",\"nextTime\":\"8-9\"}\n"
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/data-records/import?filename=accounts.jsonl", strings.NewReader(body))
	h.dataRecordsStore().ImportDataRecords(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("import seed records status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	return authIndex, h
}

func codexQuotaSyncCallAPICall(t *testing.T, h *Handler, authIndex string, upstreamURL string) {
	t.Helper()
	body := strings.NewReader(`{"auth_index":"` + authIndex + `","method":"GET","url":"` + upstreamURL + `/wham/usage","header":{"Authorization":"Bearer $TOKEN$"}}`)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/api-call", body)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.APICall(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("APICall status = %d, body %s", recorder.Code, recorder.Body.String())
	}
}

func dataRecordsByEmailForTest(t *testing.T, h *Handler) (nextTime, quota, status, health map[string]string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/data-records?limit=200", nil)
	h.dataRecordsStore().ListDataRecords(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list data records status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Total   int                      `json:"total"`
		Records []datarecords.DataRecord `json:"records"`
	}
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode data records list: %v", errDecode)
	}
	nextTime = map[string]string{}
	quota = map[string]string{}
	status = map[string]string{}
	health = map[string]string{}
	for _, record := range payload.Records {
		data, okData := record.Data.(map[string]any)
		if !okData {
			continue
		}
		email, _ := data["email"].(string)
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		if value, ok := data["nextTime"].(string); ok {
			nextTime[email] = value
		}
		if value, ok := data["quota"].(string); ok {
			quota[email] = value
		}
		if value, ok := data["status"].(string); ok {
			status[email] = value
		}
		if value, ok := data["health"].(string); ok {
			health[email] = value
		}
	}
	return nextTime, quota, status, health
}

// codexQuotaExpectedNextTime renders the expected "M-D HH:MM" value for a unix
// reset timestamp exactly the way the handler formats it, in local time.
func codexQuotaExpectedNextTime(unix int64) string {
	reset := time.Unix(unix, 0).In(time.Local)
	return fmt.Sprintf("%d-%d %02d:%02d", int(reset.Month()), reset.Day(), reset.Hour(), reset.Minute())
}

func TestAPICallCodexQuotaRefreshSyncsDataRecordQuotaAndNextTimeByEmail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wham/usage" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rate_limits":[{"name":"primary","limit_window_seconds":18000,"reset_at":1786237200,"used_percent":12.5},{"name":"monthly","limit_window_seconds":2592000,"reset_at":1788654360,"used_percent":42.37}]}`))
	}))
	defer upstream.Close()

	authIndex, h := codexQuotaSyncFixture(t)
	codexQuotaSyncCallAPICall(t, h, authIndex, upstream.URL)

	gotNextTime, gotQuota, _, gotHealth := dataRecordsByEmailForTest(t, h)
	if want := codexQuotaExpectedNextTime(1788654360); gotNextTime["target@example.test"] != want {
		t.Fatalf("target nextTime = %q, want %q (longest reset window wins)", gotNextTime["target@example.test"], want)
	}
	if gotQuota["target@example.test"] != "57.63%" {
		t.Fatalf("target quota = %q, want 57.63%%", gotQuota["target@example.test"])
	}
	if gotHealth["target@example.test"] != "ok" {
		t.Fatalf("target health = %q, want ok", gotHealth["target@example.test"])
	}
	if gotNextTime["other@example.test"] != "8-9" {
		t.Fatalf("other nextTime = %q, want unchanged 8-9", gotNextTime["other@example.test"])
	}
	if gotQuota["other@example.test"] != "" {
		t.Fatalf("other quota = %q, want empty", gotQuota["other@example.test"])
	}
}

func TestAPICallCodexQuotaRefreshFailureMarksDataRecordStatusErr(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wham/usage" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		http.Error(w, "Your authentication token has been invalidated. Please try signing in again.", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	authIndex, h := codexQuotaSyncFixture(t)
	codexQuotaSyncCallAPICall(t, h, authIndex, upstream.URL)

	gotNextTime, _, _, gotHealth := dataRecordsByEmailForTest(t, h)
	if gotHealth["target@example.test"] != "token_invalidated" {
		t.Fatalf("target health = %q, want token_invalidated", gotHealth["target@example.test"])
	}
	if gotNextTime["target@example.test"] != "8-8" {
		t.Fatalf("target nextTime = %q, want unchanged 8-8", gotNextTime["target@example.test"])
	}
	if gotHealth["other@example.test"] != "unknown" {
		t.Fatalf("other health = %q, want untouched unknown", gotHealth["other@example.test"])
	}
}

func TestCodexQuotaFailureClassification(t *testing.T) {
	cases := []struct {
		statusCode int
		body       string
		want       string
	}{
		{http.StatusUnauthorized, `{"detail":"Provided authentication token is expired. Please try signing in again."}`, "token_expired"},
		{http.StatusUnauthorized, "Your authentication token has been invalidated. Please try signing in again.", "token_invalidated"},
		{http.StatusUnauthorized, `{"error":"nope"}`, "token_invalid"},
		{http.StatusPaymentRequired, `{"detail":{"code":"deactivated_workspace"}}`, "workspace_deactivated"},
		{http.StatusInternalServerError, "boom", "err"},
	}
	for _, tc := range cases {
		if got := classifyCodexQuotaFailure(tc.statusCode, tc.body); got != tc.want {
			t.Fatalf("classify(%d, %q) = %q, want %q", tc.statusCode, tc.body, got, tc.want)
		}
	}
}

func TestAPICallCodexQuotaEmailHintWritesHealthWithoutAuthIndex(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wham/usage" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer raw-token-value" {
			t.Errorf("upstream Authorization = %q, want raw token passed through", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rate_limits":[{"limit_window_seconds":2592000,"reset_at":1788654360,"used_percent":100}]}`))
	}))
	defer upstream.Close()

	_, h := codexQuotaSyncFixture(t)
	body := strings.NewReader(`{"method":"GET","url":"` + upstream.URL + `/wham/usage","header":{"Authorization":"Bearer raw-token-value"}}`)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/api-call", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cpa-Data-Email", "target@example.test")
	ctx.Request = req

	h.APICall(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("APICall status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	gotNextTime, gotQuota, _, gotHealth := dataRecordsByEmailForTest(t, h)
	if gotHealth["target@example.test"] != "depleted" {
		t.Fatalf("target health = %q, want depleted (used_percent=100)", gotHealth["target@example.test"])
	}
	if gotQuota["target@example.test"] != "0%" {
		t.Fatalf("target quota = %q, want 0%%", gotQuota["target@example.test"])
	}
	if want := codexQuotaExpectedNextTime(1788654360); gotNextTime["target@example.test"] != want {
		t.Fatalf("target nextTime = %q, want %q", gotNextTime["target@example.test"], want)
	}
}

func TestCodexQuotaSnapshotFromBodyPicksLongestResetWindow(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	body := `{"rate_limits":[{"limit_window_seconds":18000,"reset_after_seconds":3600,"used_percent":10},{"limit_window_seconds":2592000,"reset_at":1788654360,"used_percent":42.37}]}`
	snapshot, ok := codexQuotaSnapshotFromBody(body, now)
	if !ok {
		t.Fatal("expected quota snapshot from body")
	}
	if want := codexQuotaExpectedNextTime(1788654360); snapshot.NextTime != want {
		t.Fatalf("nextTime = %q, want %q", snapshot.NextTime, want)
	}
	if snapshot.Quota != "57.63%" {
		t.Fatalf("quota = %q, want 57.63%%", snapshot.Quota)
	}
}

func TestCodexQuotaSnapshotFromBodyRejectsNonQuotaPayload(t *testing.T) {
	if _, ok := codexQuotaSnapshotFromBody("not-json", time.Now()); ok {
		t.Fatal("invalid json body should not parse")
	}
	if _, ok := codexQuotaSnapshotFromBody(`{"foo":"bar"}`, time.Now()); ok {
		t.Fatal("payload without reset fields should not parse")
	}
	if _, ok := codexQuotaSnapshotFromBody("", time.Now()); ok {
		t.Fatal("empty body should not parse")
	}
}
