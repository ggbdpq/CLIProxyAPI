package datarecords

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDataRecordsDBPathUsesDataDirectory(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data records db path: %v", err)
	}
	want := filepath.Join(tmp, "data", "data-records.sqlite")
	if dbPath != want {
		t.Fatalf("db path = %q, want %q", dbPath, want)
	}
	if info, err := os.Stat(filepath.Dir(dbPath)); err != nil || !info.IsDir() {
		t.Fatalf("data directory was not created: info=%v err=%v", info, err)
	}
}

func TestDataRecordsDBPathMovesLegacyDatabaseIntoDataDirectory(t *testing.T) {
	tmp := t.TempDir()
	legacyDir := filepath.Join(tmp, ".cli-proxy-api")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("create legacy dir: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "data-records.sqlite")
	if err := os.WriteFile(legacyPath, []byte("legacy-db"), 0o600); err != nil {
		t.Fatalf("write legacy db: %v", err)
	}
	h := New(filepath.Join(tmp, "config.yaml"), "")

	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data records db path: %v", err)
	}
	want := filepath.Join(tmp, "data", "data-records.sqlite")
	if dbPath != want {
		t.Fatalf("db path = %q, want %q", dbPath, want)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read moved db: %v", err)
	}
	if string(data) != "legacy-db" {
		t.Fatalf("moved db content = %q", string(data))
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy db still exists or stat failed unexpectedly: %v", err)
	}
}

func TestImportDataRecordsJSONLPersistsAndListsFromSQLite(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sample.jsonl")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err = part.Write([]byte("{\"name\":\"alpha\",\"score\":1}\n\n{\"name\":\"beta\",\"score\":2}\n")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/data-records/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Request = req

	h.ImportDataRecords(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var importPayload map[string]any
	if err = json.Unmarshal(rec.Body.Bytes(), &importPayload); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if got := int(importPayload["imported"].(float64)); got != 2 {
		t.Fatalf("imported = %d, want 2", got)
	}

	listRec := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRec)
	listReq := httptest.NewRequest(http.MethodGet, "/v0/management/data-records?limit=10", nil)
	listCtx.Request = listReq

	h.ListDataRecords(listCtx)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", listRec.Code, listRec.Body.String())
	}
	var listPayload struct {
		Total   int `json:"total"`
		Records []struct {
			ID         int64           `json:"id"`
			SourceFile string          `json:"source_file"`
			LineNumber int             `json:"line_number"`
			Summary    map[string]any  `json:"summary"`
			Data       json.RawMessage `json:"data"`
		} `json:"records"`
	}
	if err = json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listPayload.Total != 2 || len(listPayload.Records) != 2 {
		t.Fatalf("listed total=%d len=%d, want 2", listPayload.Total, len(listPayload.Records))
	}
	if listPayload.Records[0].SourceFile != "sample.jsonl" || listPayload.Records[0].LineNumber != 3 {
		t.Fatalf("first listed record metadata = %#v", listPayload.Records[0])
	}
	var first map[string]any
	if err = json.Unmarshal(listPayload.Records[0].Data, &first); err != nil {
		t.Fatalf("decode first data: %v", err)
	}
	if first["name"] != "beta" {
		t.Fatalf("first record name = %#v, want beta", first["name"])
	}
}

func TestListDataRecordsOrdersByNextTimeAscending(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data records db path: %v", err)
	}
	records, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"first@example.test","nextTime":"9-6 09:41"}`,
		`{"email":"second@example.test","nextTime":"8-13 16:45"}`,
		`{"email":"third@example.test","nextTime":"10-1 08:00"}`,
		`{"email":"fourth@example.test","nextTime":"9-9 11:01"}`,
	}, "\n")), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	payload := listDataRecordsForTest(t, h)
	got := make([]string, 0, len(payload.Records))
	for _, record := range payload.Records {
		var data map[string]string
		if err := json.Unmarshal(record.Data, &data); err != nil {
			t.Fatalf("decode record data: %v", err)
		}
		got = append(got, data["nextTime"])
	}
	want := []string{"8-13 16:45", "9-6 09:41", "9-9 11:01", "10-1 08:00"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("nextTime order = %v, want %v", got, want)
	}
}

func TestListDataRecordsFiltersByEmailAndNextTime(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data records db path: %v", err)
	}
	records, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"account_claims_email":"alpha@example.test","nextTime":"8-13 16:45","status":"ok"}`,
		`{"email":"bravo@example.test","nextTime":"9-6 09:41","status":"err"}`,
		`{"email":"charlie@example.test","nextTime":"10-1 08:00","status":"ok"}`,
	}, "\n")), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	emailPayload := listDataRecordsForTestQuery(t, h, "q=alpha")
	if emailPayload.Total != 1 || len(emailPayload.Records) != 1 {
		t.Fatalf("email filtered total=%d len=%d, want 1", emailPayload.Total, len(emailPayload.Records))
	}
	var emailData map[string]string
	if err := json.Unmarshal(emailPayload.Records[0].Data, &emailData); err != nil {
		t.Fatalf("decode email filtered data: %v", err)
	}
	if emailData["account_claims_email"] != "alpha@example.test" {
		t.Fatalf("email filtered record = %#v, want alpha@example.test", emailData)
	}

	nextTimePayload := listDataRecordsForTestQuery(t, h, "q=9-6")
	if nextTimePayload.Total != 1 || len(nextTimePayload.Records) != 1 {
		t.Fatalf("nextTime filtered total=%d len=%d, want 1", nextTimePayload.Total, len(nextTimePayload.Records))
	}
	var nextTimeData map[string]string
	if err := json.Unmarshal(nextTimePayload.Records[0].Data, &nextTimeData); err != nil {
		t.Fatalf("decode nextTime filtered data: %v", err)
	}
	if nextTimeData["email"] != "bravo@example.test" {
		t.Fatalf("nextTime filtered record = %#v, want bravo@example.test", nextTimeData)
	}

	statusPayload := listDataRecordsForTestQuery(t, h, "q=err")
	if statusPayload.Total != 1 || len(statusPayload.Records) != 1 {
		t.Fatalf("status filtered total=%d len=%d, want 1", statusPayload.Total, len(statusPayload.Records))
	}
	var statusData map[string]string
	if err := json.Unmarshal(statusPayload.Records[0].Data, &statusData); err != nil {
		t.Fatalf("decode status filtered data: %v", err)
	}
	if statusData["email"] != "bravo@example.test" {
		t.Fatalf("status filtered record = %#v, want bravo@example.test", statusData)
	}
}

func TestParseJSONLDataRecordsAcceptsUTF8BOMAccountLine(t *testing.T) {
	line := []byte("\xef\xbb\xbf" + `{"version":1,"db_id":3378,"platform":"chatgpt","email":"user@example.test","access_token":"token-value","refresh_token":"refresh-value","mailbox":{"provider":"icloud-show","state":"verified"}}` + "\n")
	records, err := parseJSONLDataRecords(bytes.NewReader(line), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse jsonl with bom: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	if records[0].LineNumber != 1 || records[0].SourceFile != "accounts.jsonl" {
		t.Fatalf("record metadata = %#v", records[0])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(records[0].Data), &data); err != nil {
		t.Fatalf("decode canonical data: %v", err)
	}
	if data["platform"] != "chatgpt" || data["email"] != "user@example.test" {
		t.Fatalf("canonical data = %#v", data)
	}
}

func TestImportDataRecordsAccountFormatReturnsSummaryAndRawData(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	record := map[string]any{
		"version":            1,
		"db_id":              2762,
		"platform":           "chatgpt",
		"email":              "user@example.test",
		"password":           "plain-password-value",
		"access_token":       "access-token-value",
		"refresh_token":      "refresh-token-value",
		"id_token":           "id-token-value",
		"status":             "registered",
		"source":             "login",
		"chatgpt_user_id":    "user-123",
		"organization_id":    "org-123",
		"project_id":         "project-123",
		"mailbox_connection": "user@example.test----https://mail.example.test/latest?api_key=secret-key",
		"mailbox": map[string]any{
			"provider":      "microsoft",
			"enabled":       false,
			"refresh_token": "mail-refresh-token",
			"mailapi_url":   "https://mail.example.test/latest?api_key=secret-key",
		},
	}
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal account record: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "accounts.jsonl")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err = part.Write(append(line, '\n')); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/data-records/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Request = req
	h.ImportDataRecords(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRec)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/data-records", nil)
	h.ListDataRecords(listCtx)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", listRec.Code, listRec.Body.String())
	}

	var listPayload struct {
		Records []struct {
			Summary map[string]any  `json:"summary"`
			Data    json.RawMessage `json:"data"`
		} `json:"records"`
	}
	if err = json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listPayload.Records) != 1 {
		t.Fatalf("records len = %d, want 1", len(listPayload.Records))
	}
	if listPayload.Records[0].Summary["platform"] != "chatgpt" || listPayload.Records[0].Summary["email"] != "user@example.test" {
		t.Fatalf("summary = %#v", listPayload.Records[0].Summary)
	}
	if listPayload.Records[0].Summary["mailbox_provider"] != "microsoft" || listPayload.Records[0].Summary["mailbox_enabled"] != false {
		t.Fatalf("mailbox summary = %#v", listPayload.Records[0].Summary)
	}
	var listed map[string]any
	if err = json.Unmarshal(listPayload.Records[0].Data, &listed); err != nil {
		t.Fatalf("decode listed account data: %v", err)
	}
	if listed["password"] != "plain-password-value" || listed["access_token"] != "access-token-value" {
		t.Fatalf("expected raw account secrets to be visible in data management response")
	}
	mailbox, ok := listed["mailbox"].(map[string]any)
	if !ok || mailbox["mailapi_url"] != "https://mail.example.test/latest?api_key=secret-key" {
		t.Fatalf("expected raw mailbox data to be visible, got %#v", listed["mailbox"])
	}
}

func TestImportDataRecordsRejectsInvalidJSONLWithoutPartialRows(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "broken.jsonl")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err = part.Write([]byte("{\"ok\":true}\n{broken}\n")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/data-records/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Request = req

	h.ImportDataRecords(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import status = %d, want %d, body %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRec)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/data-records", nil)
	h.ListDataRecords(listCtx)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", listRec.Code, listRec.Body.String())
	}
	var listPayload struct {
		Total int `json:"total"`
	}
	if err = json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listPayload.Total != 0 {
		t.Fatalf("total = %d, want 0", listPayload.Total)
	}
}

func TestDeleteDataRecordsRemovesSelectedRowsFromSQLite(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	records, err := parseJSONLDataRecords(bytes.NewBufferString("{\"name\":\"alpha\"}\n{\"name\":\"beta\"}\n{\"name\":\"gamma\"}\n"), "sample.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	listBefore := listDataRecordsForTest(t, h)
	if listBefore.Total != 3 || len(listBefore.Records) != 3 {
		t.Fatalf("before delete total=%d len=%d, want 3", listBefore.Total, len(listBefore.Records))
	}
	deleteID := listBefore.Records[1].ID

	body := bytes.NewBufferString(fmt.Sprintf(`{"ids":[%d]}`, deleteID))
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodDelete, "/v0/management/data-records", body)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.DeleteDataRecords(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body %s", rec.Code, rec.Body.String())
	}
	var deletePayload struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &deletePayload); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if deletePayload.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deletePayload.Deleted)
	}

	listAfter := listDataRecordsForTest(t, h)
	if listAfter.Total != 2 || len(listAfter.Records) != 2 {
		t.Fatalf("after delete total=%d len=%d, want 2", listAfter.Total, len(listAfter.Records))
	}
	for _, record := range listAfter.Records {
		if record.ID == deleteID {
			t.Fatalf("deleted id %d still listed", deleteID)
		}
	}
}

func TestDeleteDataRecordsRemovesAllRowsWhenAllIsTrue(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	records, err := parseJSONLDataRecords(bytes.NewBufferString("{\"name\":\"alpha\"}\n{\"name\":\"beta\"}\n{\"name\":\"gamma\"}\n"), "sample.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodDelete, "/v0/management/data-records", bytes.NewBufferString(`{"all":true}`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.DeleteDataRecords(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete all status = %d, body %s", rec.Code, rec.Body.String())
	}
	var deletePayload struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &deletePayload); err != nil {
		t.Fatalf("decode delete all response: %v", err)
	}
	if deletePayload.Deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deletePayload.Deleted)
	}
	listAfter := listDataRecordsForTest(t, h)
	if listAfter.Total != 0 || len(listAfter.Records) != 0 {
		t.Fatalf("after delete all total=%d len=%d, want 0", listAfter.Total, len(listAfter.Records))
	}
}

func TestGenerateQuotaFilesWritesSelectedRecordsByEmailAndOverwrites(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	records, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"target@example.test","access_token":"old-token","type":"codex","expired":"2026-08-13T00:00:00.000+08:00","id_token":"old-id-token","refresh_token":"old-refresh-token","last_refresh":"2026-08-05T10:00:00.000+08:00","db_id":1}`,
		`{"email":"other@example.test","access_token":"other-token","type":"codex","expired":"2026-08-14T00:00:00.000+08:00","id_token":"other-id-token","refresh_token":"other-refresh-token","last_refresh":"2026-08-05T11:00:00.000+08:00"}`,
		`{"email":"target@example.test","access_token":"new-token","type":"codex","expired":"2026-08-15T00:00:00.000+08:00","id_token":"new-id-token","refresh_token":"new-refresh-token","last_refresh":"2026-08-05T12:00:00.000+08:00","db_id":2,"nextTime":"9-5 20:18"}`,
	}, "\n")+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	listBefore := listDataRecordsForTest(t, h)
	var oldID int64
	var newID int64
	for _, record := range listBefore.Records {
		var data map[string]any
		if err := json.Unmarshal(record.Data, &data); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		if data["access_token"] == "old-token" {
			oldID = record.ID
		}
		if data["access_token"] == "new-token" {
			newID = record.ID
		}
	}
	if oldID == 0 || newID == 0 {
		t.Fatalf("missing imported record ids old=%d new=%d", oldID, newID)
	}

	body := bytes.NewBufferString(fmt.Sprintf(`{"ids":[%d,%d]}`, oldID, newID))
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/data-records/generate-quota", body)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.GenerateQuotaFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("generate status = %d, body %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Exported  int      `json:"exported"`
		OutputDir string   `json:"output_dir"`
		Files     []string `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode generate response: %v", err)
	}
	if payload.Exported != 2 {
		t.Fatalf("exported = %d, want 2", payload.Exported)
	}
	wantDir := filepath.Join(tmp, ".cli-proxy-api")
	if payload.OutputDir != wantDir {
		t.Fatalf("output dir = %q, want %q", payload.OutputDir, wantDir)
	}
	if len(payload.Files) != 1 || payload.Files[0] != "target@example.test.json" {
		t.Fatalf("files = %#v, want target file once", payload.Files)
	}
	data, err := os.ReadFile(filepath.Join(wantDir, "target@example.test.json"))
	if err != nil {
		t.Fatalf("read generated quota file: %v", err)
	}
	var generated map[string]any
	if err := json.Unmarshal(data, &generated); err != nil {
		t.Fatalf("decode generated quota file: %v", err)
	}
	want := map[string]any{
		"type":          "codex",
		"email":         "target@example.test",
		"expired":       "2026-08-15T00:00:00.000+08:00",
		"id_token":      "new-id-token",
		"account_id":    "",
		"disabled":      false,
		"access_token":  "new-token",
		"last_refresh":  "2026-08-05T12:00:00.000+08:00",
		"refresh_token": "new-refresh-token",
	}
	if len(generated) != len(want) {
		t.Fatalf("generated field count = %d, want %d, data %#v", len(generated), len(want), generated)
	}
	for key, wantValue := range want {
		if generated[key] != wantValue {
			t.Fatalf("generated[%s] = %#v, want %#v", key, generated[key], wantValue)
		}
	}
	if !bytes.Contains(data, []byte("\n  \"access_token\"")) {
		t.Fatalf("generated quota file should be pretty-printed, got %s", string(data))
	}
	if _, err := os.Stat(filepath.Join(wantDir, "other@example.test.json")); !os.IsNotExist(err) {
		t.Fatalf("unselected record file should not exist, stat err=%v", err)
	}
	after := listDataRecordsForTest(t, h)
	marked := 0
	for _, record := range after.Records {
		var row map[string]any
		if err := json.Unmarshal(record.Data, &row); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		if row["email"] != "target@example.test" {
			continue
		}
		marked++
		if row["lifecycle"] != "in_use" || row["deploy_target"] != "local" || row["deployed_at"] == "" {
			t.Fatalf("legacy generate should mark local deploy fields: %#v", row)
		}
	}
	if marked != 2 {
		t.Fatalf("marked target records = %d, want 2", marked)
	}
}

func TestDeployDataRecordsWritesFilesAndMarksRecords(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	records, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"target@example.test","access_token":"token","type":"codex","id_token":"id-token","refresh_token":"refresh-token","lifecycle":"unused"}`,
		`{"email":"target@example.test","access_token":"new-token","type":"codex","id_token":"new-id-token","refresh_token":"new-refresh-token","lifecycle":"unused"}`,
	}, "\n")+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}
	listed := listDataRecordsForTest(t, h)
	ids := []int64{listed.Records[0].ID, listed.Records[1].ID, listed.Records[1].ID}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/data-records/deploy", bytes.NewBufferString(fmt.Sprintf(`{"ids":[%d,%d,%d],"target":"local"}`, ids[0], ids[1], ids[2])))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.DeployDataRecords(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("deploy status = %d, body %s", rec.Code, rec.Body.String())
	}
	var payload dataRecordsDeployResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode deploy response: %v", err)
	}
	wantDir := filepath.Join(tmp, ".cli-proxy-api")
	if payload.Deployed != 2 || payload.OutputDir != wantDir || payload.Target != "local" {
		t.Fatalf("deploy payload = %#v", payload)
	}
	if len(payload.Files) != 1 || payload.Files[0] != "target@example.test.json" {
		t.Fatalf("files = %#v, want target file once", payload.Files)
	}
	data, err := os.ReadFile(filepath.Join(wantDir, "target@example.test.json"))
	if err != nil {
		t.Fatalf("read deployed file: %v", err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("decode deployed file: %v", err)
	}
	if file["email"] != "target@example.test" || file["access_token"] == "" || file["refresh_token"] == "" {
		t.Fatalf("deployed file = %#v", file)
	}

	after := listDataRecordsForTest(t, h)
	for _, record := range after.Records {
		var row map[string]any
		if err := json.Unmarshal(record.Data, &row); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		if row["lifecycle"] != "in_use" || row["deploy_target"] != "local" || row["deployed_at"] == "" {
			t.Fatalf("record deploy fields = %#v", row)
		}
	}
}

func TestRecycleDataRecordsDeletesFilesAndClearsDeployFields(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	records, err := parseJSONLDataRecords(bytes.NewBufferString(`{"email":"target@example.test","access_token":"token","refresh_token":"refresh-token","lifecycle":"unused"}`+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}
	id := listDataRecordsForTest(t, h).Records[0].ID
	var deployed dataRecordsDeployResponse
	if err := runDataRecordsSQLite(dataRecordsDeployRequest{Action: "deploy", DBPath: dbPath, OutputDir: filepath.Join(tmp, ".cli-proxy-api"), Target: "local", IDs: []int64{id}}, &deployed); err != nil {
		t.Fatalf("deploy fixture: %v", err)
	}
	filePath := filepath.Join(tmp, ".cli-proxy-api", "target@example.test.json")
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("deployed file missing: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/data-records/recycle", bytes.NewBufferString(fmt.Sprintf(`{"ids":[%d],"target":"local"}`, id)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.RecycleDataRecords(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("recycle status = %d, body %s", rec.Code, rec.Body.String())
	}
	var payload dataRecordsRecycleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode recycle response: %v", err)
	}
	if payload.Recycled != 1 || payload.OutputDir != filepath.Join(tmp, ".cli-proxy-api") || payload.Target != "local" {
		t.Fatalf("recycle payload = %#v", payload)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("deployed file should be removed, stat err=%v", err)
	}

	after := listDataRecordsForTest(t, h)
	var row map[string]any
	if err := json.Unmarshal(after.Records[0].Data, &row); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if row["lifecycle"] != "unused" {
		t.Fatalf("lifecycle = %#v, want unused", row["lifecycle"])
	}
	if _, ok := row["deployed_at"]; ok {
		t.Fatalf("deployed_at should be cleared: %#v", row)
	}
	if _, ok := row["deploy_target"]; ok {
		t.Fatalf("deploy_target should be cleared: %#v", row)
	}
}

func TestDeployDataRecordsRejectsUnknownTarget(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/data-records/deploy", bytes.NewBufferString(`{"ids":[1],"target":"../elsewhere"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.DeployDataRecords(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("deploy status = %d, want 400, body %s", rec.Code, rec.Body.String())
	}
}

func TestConvertDataRecordsContentTXTToCPA(t *testing.T) {
	payload, err := convertDataRecordsContent("txt", "cpa", "卡密导出\n"+`{"email":"a@example.test","access_token":"at","refresh_token":"rt","disabled":false}`+"\n", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("convert txt to cpa: %v", err)
	}
	if payload.Converted != 1 || payload.Filename != "cpa-records.jsonl" {
		t.Fatalf("payload = %#v", payload)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload.Content)), &row); err != nil {
		t.Fatalf("decode cpa output: %v", err)
	}
	if row["email"] != "a@example.test" || row["access_token"] != "at" || row["refresh_token"] != "rt" || row["type"] != "codex" || row["disabled"] != false {
		t.Fatalf("cpa row = %#v", row)
	}
}

func TestConvertDataRecordsContentCPAToSub2API(t *testing.T) {
	payload, err := convertDataRecordsContent("cpa", "sub2api", `{"email":"a@example.test","access_token":"at","refresh_token":"rt","id_token":"it","expired":"2026-09-30T00:00:00Z","source_batch":"batch-1"}`+"\n", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("convert cpa to sub2api: %v", err)
	}
	var bundle struct {
		ExportedAt string `json:"exported_at"`
		Accounts   []struct {
			Name        string         `json:"name"`
			Platform    string         `json:"platform"`
			Type        string         `json:"type"`
			Credentials map[string]any `json:"credentials"`
			Extra       map[string]any `json:"extra"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(payload.Content), &bundle); err != nil {
		t.Fatalf("decode sub2api output: %v", err)
	}
	if payload.Converted != 1 || payload.Filename != "sub2api-accounts.json" || bundle.ExportedAt != "2026-09-01T00:00:00Z" || len(bundle.Accounts) != 1 {
		t.Fatalf("payload/bundle = %#v %#v", payload, bundle)
	}
	account := bundle.Accounts[0]
	if account.Name != "a@example.test" || account.Platform != "openai" || account.Type != "oauth" {
		t.Fatalf("account meta = %#v", account)
	}
	if account.Credentials["email"] != "a@example.test" || account.Credentials["access_token"] != "at" || account.Credentials["refresh_token"] != "rt" || account.Credentials["expires_at"] != "2026-09-30T00:00:00Z" || account.Credentials["client_id"] != defaultCodexClientID {
		t.Fatalf("credentials = %#v", account.Credentials)
	}
	if account.Extra["source_batch"] != "batch-1" {
		t.Fatalf("extra = %#v", account.Extra)
	}
}

func TestConvertDataRecordsContentSub2APIToCPA(t *testing.T) {
	input := `{"accounts":[{"name":"a@example.test","platform":"openai","type":"oauth","credentials":{"email":"a@example.test","access_token":"at","refresh_token":"rt","expires_at":"2026-09-30T00:00:00Z"},"extra":{"source":"sub2api"}}]}`
	payload, err := convertDataRecordsContent("sub2api", "cpa", input, time.Now())
	if err != nil {
		t.Fatalf("convert sub2api to cpa: %v", err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload.Content)), &row); err != nil {
		t.Fatalf("decode cpa output: %v", err)
	}
	if row["email"] != "a@example.test" || row["access_token"] != "at" || row["refresh_token"] != "rt" || row["expired"] != "2026-09-30T00:00:00Z" || row["type"] != "codex" || row["source"] != "sub2api" {
		t.Fatalf("cpa row = %#v", row)
	}
}

func TestRegisterDataRecordReauthUpdatesExistingAndImportsUnmatched(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	seed, err := parseJSONLDataRecords(bytes.NewBufferString(`{"email":"a@example.test","access_token":"old","refresh_token":"old-rt","quota":"57%","nextTime":"8-8","health":"token_invalid","auth_state":"needs_reauth"}`+"\n"), "seed.jsonl")
	if err != nil {
		t.Fatalf("parse seed: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: seed}, nil); err != nil {
		t.Fatalf("import seed: %v", err)
	}
	reauth, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"a@example.test","access_token":"new","refresh_token":"new-rt","id_token":"new-it","expired":"2026-10-01T00:00:00Z","last_refresh":"2026-09-01T00:00:00Z","reauth_at":"2026-09-01T01:00:00Z"}`,
		`{"email":"b@example.test","access_token":"b-at","refresh_token":"b-rt","expired":"2026-10-02T00:00:00Z"}`,
		`{"access_token":"missing-email"}`,
		`{"email":"missing-token@example.test"}`,
	}, "\n")+"\n"), "reauth.jsonl")
	if err != nil {
		t.Fatalf("parse reauth: %v", err)
	}
	var stats dataRecordsRegisterReauthResponse
	if err := runDataRecordsSQLite(dataRecordsRegisterReauthRequest{Action: "register_reauth", DBPath: dbPath, Records: reauth, ImportUnmatched: true}, &stats); err != nil {
		t.Fatalf("register reauth: %v", err)
	}
	if stats.Total != 4 || stats.Updated != 1 || stats.Imported != 1 || stats.Unmatched != 1 || stats.MissingEmail != 1 || stats.MissingToken != 1 || stats.Skipped != 0 {
		t.Fatalf("reauth stats = %#v", stats)
	}
	listed := listDataRecordsForTest(t, h)
	rows := map[string]map[string]any{}
	for _, record := range listed.Records {
		var row map[string]any
		if err := json.Unmarshal(record.Data, &row); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		rows[row["email"].(string)] = row
	}
	updated := rows["a@example.test"]
	if updated["access_token"] != "new" || updated["refresh_token"] != "new-rt" || updated["id_token"] != "new-it" || updated["auth_state"] != "authorized" || updated["health"] != "unknown" {
		t.Fatalf("updated row = %#v", updated)
	}
	if updated["reset_expired_at"] != "2026-10-01T00:00:00Z" || updated["reauth_at"] != "2026-09-01T01:00:00Z" {
		t.Fatalf("reauth metadata = %#v", updated)
	}
	if _, ok := updated["quota"]; ok {
		t.Fatalf("quota should be cleared: %#v", updated)
	}
	if rows["b@example.test"]["auth_state"] != "authorized" || rows["b@example.test"]["reset_expired_at"] != "2026-10-02T00:00:00Z" {
		t.Fatalf("imported row = %#v", rows["b@example.test"])
	}
}

func TestRefreshDataRecordTokenRotatesTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "rt-old" || r.Form.Get("client_id") != defaultCodexClientID {
			t.Errorf("unexpected refresh form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-new","id_token":"it-new","refresh_token":"rt-new","expires_in":3600}`))
	}))
	defer srv.Close()
	oldURL := openAITokenURL
	openAITokenURL = srv.URL
	defer func() { openAITokenURL = oldURL }()

	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	records, err := parseJSONLDataRecords(bytes.NewBufferString(`{"email":"a@example.test","access_token":"at-old","refresh_token":"rt-old","id_token":"it-old","quota":"57.63%","nextTime":"8-8","health":"token_invalid"}`+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}
	id := listDataRecordsForTest(t, h).Records[0].ID

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/data-records/refresh-token", bytes.NewBufferString(fmt.Sprintf(`{"id":%d}`, id)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.RefreshDataRecordToken(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body %s", rec.Code, rec.Body.String())
	}

	after := listDataRecordsForTest(t, h)
	raw, errMarshal := json.Marshal(after.Records[0].Data)
	if errMarshal != nil {
		t.Fatalf("marshal record data: %v", errMarshal)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if data["access_token"] != "at-new" || data["refresh_token"] != "rt-new" || data["id_token"] != "it-new" {
		t.Fatalf("rotated tokens = %#v", data)
	}
	if data["expired"] == "" || data["last_refresh"] == "" || after.Records[0].TokenExp == nil {
		t.Fatalf("expiry metadata missing: data=%#v token_exp=%v", data, after.Records[0].TokenExp)
	}
	if data["health"] != "unknown" {
		t.Fatalf("health = %#v, want reset to unknown", data["health"])
	}
	if _, hasQuota := data["quota"]; hasQuota {
		t.Fatalf("stale quota should be cleared: %#v", data)
	}
}

func TestRefreshDataRecordTokenMarksNeedsReauthOnInvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"The refresh token is invalid or expired."}`))
	}))
	defer srv.Close()
	oldURL := openAITokenURL
	openAITokenURL = srv.URL
	defer func() { openAITokenURL = oldURL }()

	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	records, err := parseJSONLDataRecords(bytes.NewBufferString(`{"email":"dead@example.test","access_token":"at-old","refresh_token":"rt-dead"}`+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}
	id := listDataRecordsForTest(t, h).Records[0].ID

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/data-records/refresh-token", bytes.NewBufferString(fmt.Sprintf(`{"id":%d}`, id)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.RefreshDataRecordToken(ctx)
	if rec.Code != http.StatusConflict {
		t.Fatalf("refresh status = %d, want 409, body %s", rec.Code, rec.Body.String())
	}

	after := listDataRecordsForTest(t, h)
	raw, errMarshal := json.Marshal(after.Records[0].Data)
	if errMarshal != nil {
		t.Fatalf("marshal record data: %v", errMarshal)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if data["auth_state"] != "needs_reauth" || data["access_token"] != "at-old" || data["refresh_token"] != "rt-dead" {
		t.Fatalf("record after failed refresh = %#v", data)
	}
}

func TestUpdateTokensByEmailWritesCredentialFields(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	records, err := parseJSONLDataRecords(bytes.NewBufferString(`{"email":"a@example.test","access_token":"old-at","refresh_token":"old-rt","quota":"57%","health":"token_invalid"}`+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	updated, err := h.UpdateTokensByEmail("A@example.test", map[string]string{
		"access_token":  "new-at",
		"refresh_token": "new-rt",
		"id_token":      "new-it",
		"expired":       "2026-09-30T18:06:00.000+08:00",
		"last_refresh":  "2026-08-31T10:00:00.000+08:00",
	})
	if err != nil {
		t.Fatalf("update tokens: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}

	after := listDataRecordsForTest(t, h)
	raw, errMarshal := json.Marshal(after.Records[0].Data)
	if errMarshal != nil {
		t.Fatalf("marshal record data: %v", errMarshal)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if data["access_token"] != "new-at" || data["refresh_token"] != "new-rt" || data["id_token"] != "new-it" {
		t.Fatalf("tokens = %#v", data)
	}
	if data["expired"] != "2026-09-30T18:06:00.000+08:00" || data["last_refresh"] != "2026-08-31T10:00:00.000+08:00" {
		t.Fatalf("expiry metadata = %#v", data)
	}
}

func TestListDataRecordsReturnsTokenExpFromExpiredField(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	records, err := parseJSONLDataRecords(bytes.NewBufferString(`{"email":"a@example.test","expired":"2026-08-13T00:00:00Z"}`+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}
	listed := listDataRecordsForTest(t, h)
	want := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC).Unix()
	if listed.Records[0].TokenExp == nil || *listed.Records[0].TokenExp != want {
		t.Fatalf("token_exp = %v, want %d", listed.Records[0].TokenExp, want)
	}
}

type dataRecordsListPayloadForTest struct {
	Total   int `json:"total"`
	Records []struct {
		ID       int64           `json:"id"`
		Data     json.RawMessage `json:"data"`
		TokenExp *int64          `json:"token_exp,omitempty"`
	} `json:"records"`
}

func listDataRecordsForTest(t *testing.T, h *Store) dataRecordsListPayloadForTest {
	return listDataRecordsForTestQuery(t, h, "")
}

func listDataRecordsForTestQuery(t *testing.T, h *Store, query string) dataRecordsListPayloadForTest {
	t.Helper()
	listRec := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRec)
	url := "/v0/management/data-records?limit=10"
	if query != "" {
		url += "&" + query
	}
	listCtx.Request = httptest.NewRequest(http.MethodGet, url, nil)
	h.ListDataRecords(listCtx)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", listRec.Code, listRec.Body.String())
	}
	var payload dataRecordsListPayloadForTest
	if err := json.Unmarshal(listRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return payload
}

func TestListDataRecordsFiltersByThreeStatesAndBatch(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	records, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"a@example.test","source_batch":"01.2026-05-08-LD2605082GP10R-5件-1.03元","lifecycle":"unused","health":"ok","auth_state":"authorized"}`,
		`{"email":"b@example.test","source_batch":"01.2026-05-08-LD2605082GP10R-5件-1.03元","lifecycle":"sold","health":"err","auth_state":"missing_mailapi_url"}`,
		`{"email":"c@example.test","source_batch":"02.2026-05-08-LD260508O5INF3-10件-4.12元","lifecycle":"in_use","health":"depleted","auth_state":"authorized","quota":"57.63%","nextTime":"8-8"}`,
		`{"email":"d@example.test","lifecycle":"unused","health":"unknown","auth_state":"authorized"}`,
	}, "\n")+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	filterEmails := func(t *testing.T, req dataRecordsListRequest) []string {
		t.Helper()
		req.Action = "list"
		req.DBPath = dbPath
		req.Limit = 50
		var payload dataRecordsListResponse
		if err := runDataRecordsSQLite(req, &payload); err != nil {
			t.Fatalf("list with filters: %v", err)
		}
		emails := make([]string, 0, len(payload.Records))
		for _, record := range payload.Records {
			raw, errMarshal := json.Marshal(record.Data)
			if errMarshal != nil {
				t.Fatalf("marshal record data: %v", errMarshal)
			}
			var data map[string]string
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatalf("decode record: %v", err)
			}
			emails = append(emails, data["email"])
		}
		return emails
	}

	if got := filterEmails(t, dataRecordsListRequest{Lifecycle: "sold"}); len(got) != 1 || got[0] != "b@example.test" {
		t.Fatalf("lifecycle filter = %v", got)
	}
	if got := filterEmails(t, dataRecordsListRequest{Health: "abnormal"}); len(got) != 2 {
		t.Fatalf("abnormal health filter = %v, want b and c", got)
	}
	if got := filterEmails(t, dataRecordsListRequest{AuthState: "missing_mailapi_url"}); len(got) != 1 || got[0] != "b@example.test" {
		t.Fatalf("auth_state filter = %v", got)
	}
	if got := filterEmails(t, dataRecordsListRequest{Batch: "01.2026-05-08-LD2605082GP10R-5件-1.03元"}); len(got) != 2 {
		t.Fatalf("batch filter = %v, want a and b", got)
	}
	if got := filterEmails(t, dataRecordsListRequest{Lifecycle: "unused", Health: "ok"}); len(got) != 1 || got[0] != "a@example.test" {
		t.Fatalf("combined filter = %v", got)
	}
	if got := filterEmails(t, dataRecordsListRequest{Detected: true}); len(got) != 1 || got[0] != "c@example.test" {
		t.Fatalf("detected filter = %v, want only c (has quota+nextTime)", got)
	}
}

func TestDataRecordsStatsAction(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	records, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"a@example.test","lifecycle":"unused","health":"ok","auth_state":"authorized"}`,
		`{"email":"b@example.test","lifecycle":"sold","health":"err","auth_state":"missing_mailapi_url"}`,
		`{"email":"c@example.test","lifecycle":"sold","health":"ok","auth_state":"authorized","quota":"57.63%","nextTime":"8-8"}`,
	}, "\n")+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	var payload dataRecordsStatsResponse
	if err := runDataRecordsSQLite(dataRecordsListRequest{Action: "stats", DBPath: dbPath}, &payload); err != nil {
		t.Fatalf("stats action: %v", err)
	}
	if payload.Total != 3 {
		t.Fatalf("stats total = %d, want 3", payload.Total)
	}
	if payload.Lifecycle["sold"] != 2 || payload.Lifecycle["unused"] != 1 {
		t.Fatalf("lifecycle stats = %#v", payload.Lifecycle)
	}
	if payload.Health["ok"] != 2 || payload.Health["err"] != 1 {
		t.Fatalf("health stats = %#v", payload.Health)
	}
	if payload.AuthStates["authorized"] != 2 || payload.AuthStates["missing_mailapi_url"] != 1 {
		t.Fatalf("auth_state stats = %#v", payload.AuthStates)
	}
	if payload.Detected != 1 {
		t.Fatalf("detected stats = %d, want 1", payload.Detected)
	}
}

func TestUpdateDataRecordStateValidatesAndUpdates(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	records, err := parseJSONLDataRecords(bytes.NewBufferString("{\"email\":\"a@example.test\",\"lifecycle\":\"unused\"}\n{\"email\":\"b@example.test\",\"lifecycle\":\"unused\"}\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	if err := runDataRecordsSQLite(dataRecordsUpdateStateRequest{Action: "update_state", DBPath: dbPath, IDs: []int64{1}, Lifecycle: "bogus"}, nil); err == nil {
		t.Fatal("invalid lifecycle should be rejected")
	}

	var payload dataRecordsUpdateNextTimeResponse
	if err := runDataRecordsSQLite(dataRecordsUpdateStateRequest{Action: "update_state", DBPath: dbPath, IDs: []int64{1, 2}, Lifecycle: "in_use"}, &payload); err != nil {
		t.Fatalf("update state: %v", err)
	}
	if payload.Updated != 2 {
		t.Fatalf("updated = %d, want 2", payload.Updated)
	}

	listed := listDataRecordsForTest(t, h)
	for _, record := range listed.Records {
		var data map[string]string
		if err := json.Unmarshal(record.Data, &data); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		if data["lifecycle"] != "in_use" {
			t.Fatalf("record lifecycle = %#v, want in_use", data["lifecycle"])
		}
	}
}

func TestUpdateDataRecordBatchUpsertsMetadata(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	records, err := parseJSONLDataRecords(bytes.NewBufferString("{\"email\":\"a@example.test\",\"source_batch\":\"01.2026-05-08-LD2605082GP10R-5件-1.03元\"}\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	var payload dataRecordsUpdateNextTimeResponse
	if err := runDataRecordsSQLite(dataRecordsUpdateBatchRequest{Action: "update_batch", DBPath: dbPath, BatchKey: "01.2026-05-08-LD2605082GP10R-5件-1.03元", OrderURL: "https://pay.example.test/order/1", Notes: "已售出"}, &payload); err != nil {
		t.Fatalf("update batch: %v", err)
	}
	if payload.Updated != 1 {
		t.Fatalf("updated = %d, want 1", payload.Updated)
	}

	var batches dataRecordsBatchesResponse
	if err := runDataRecordsSQLite(dataRecordsListRequest{Action: "list_batches", DBPath: dbPath}, &batches); err != nil {
		t.Fatalf("list batches: %v", err)
	}
	if batches.Total != 1 || len(batches.Batches) != 1 {
		t.Fatalf("batches = %#v", batches)
	}
	batch := batches.Batches[0]
	if batch["order_url"] != "https://pay.example.test/order/1" || batch["notes"] != "已售出" {
		t.Fatalf("batch meta = %#v", batch)
	}
}

func TestUpdateDataRecordNextTimeByEmailUpdatesOnlyMatchingEmail(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	records, err := parseJSONLDataRecords(bytes.NewBufferString("{\"email\":\"target@example.test\",\"nextTime\":\"8-8\"}\n{\"email\":\"other@example.test\",\"nextTime\":\"8-9\"}\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, nil); err != nil {
		t.Fatalf("import records: %v", err)
	}

	updated, err := h.updateDataRecordNextTimeByEmail("TARGET@example.test", "9-5")
	if err != nil {
		t.Fatalf("update nextTime: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}

	listAfter := listDataRecordsForTest(t, h)
	got := map[string]string{}
	for _, record := range listAfter.Records {
		var data map[string]any
		if err := json.Unmarshal(record.Data, &data); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		got[strings.ToLower(data["email"].(string))] = data["nextTime"].(string)
	}
	if got["target@example.test"] != "9-5" {
		t.Fatalf("target nextTime = %q, want 9-5", got["target@example.test"])
	}
	if got["other@example.test"] != "8-9" {
		t.Fatalf("other nextTime = %q, want unchanged 8-9", got["other@example.test"])
	}
}

func listDataRecordBatchesForTest(t *testing.T, h *Store) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/data-records/batches", nil)
	h.ListDataRecordBatches(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("list batches status = %d, body %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode batches response: %v", err)
	}
	return payload
}

func TestImportDataRecordsRegistersBatchesAndDefaultStates(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")

	records, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"with-mail@example.test","db_id":10,"source_batch":"02.2026-05-08-LD260508O5INF3-10件-4.12元","mailbox_url":"https://mail.example.test/latest?pt=abc"}`,
		`{"email":"no-mail@example.test","db_id":11,"source_batch":"02.2026-05-08-LD260508O5INF3-10件-4.12元"}`,
		`{"email":"plain@example.test","db_id":12}`,
	}, "\n")+"\n"), "accounts.jsonl")
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}
	var stats dataRecordsImportStats
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: records}, &stats); err != nil {
		t.Fatalf("import records: %v", err)
	}
	if stats.Imported != 3 || stats.BatchesRegistered != 1 {
		t.Fatalf("import stats = %#v, want imported=3 batches_registered=1", stats)
	}

	batches := listDataRecordBatchesForTest(t, h)
	if got := int(batches["total"].(float64)); got != 1 {
		t.Fatalf("batches total = %d, want 1", got)
	}
	batch := batches["batches"].([]any)[0].(map[string]any)
	if batch["batch_key"] != "02.2026-05-08-LD260508O5INF3-10件-4.12元" {
		t.Fatalf("batch_key = %#v", batch["batch_key"])
	}
	if batch["seq"].(float64) != 2 || batch["batch_date"] != "2026-05-08" || batch["order_id"] != "LD260508O5INF3" || batch["quantity"].(float64) != 10 || batch["total_cost"] != "4.12" {
		t.Fatalf("parsed batch meta = %#v", batch)
	}
	if batch["record_count"].(float64) != 2 {
		t.Fatalf("record_count = %#v, want 2", batch["record_count"])
	}

	listed := listDataRecordsForTest(t, h)
	states := map[string]map[string]any{}
	for _, record := range listed.Records {
		var data map[string]any
		if err := json.Unmarshal(record.Data, &data); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		states[data["email"].(string)] = data
	}
	if states["with-mail@example.test"]["lifecycle"] != "unused" || states["with-mail@example.test"]["health"] != "unknown" || states["with-mail@example.test"]["auth_state"] != "authorized" {
		t.Fatalf("with-mail states = %#v", states["with-mail@example.test"])
	}
	if states["no-mail@example.test"]["auth_state"] != "missing_mailapi_url" {
		t.Fatalf("no-mail auth_state = %#v", states["no-mail@example.test"]["auth_state"])
	}
	if states["plain@example.test"]["auth_state"] != "missing_mailapi_url" {
		t.Fatalf("plain auth_state = %#v", states["plain@example.test"]["auth_state"])
	}
}

func TestImportDataRecordsDedupeByEmailKeepsHighestDbID(t *testing.T) {
	tmp := t.TempDir()
	h := New(filepath.Join(tmp, "config.yaml"), "")
	dbPath, err := h.dataRecordsDBPath()
	if err != nil {
		t.Fatalf("data db path: %v", err)
	}

	seed, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"live@example.test","db_id":1,"quota":"57.63%","nextTime":"8-8","status":"ok"}`,
		`{"email":"dbnewer@example.test","db_id":50,"access_token":"current-token"}`,
		`{"email":"gap@example.test","db_id":60}`,
	}, "\n")+"\n"), "seed.jsonl")
	if err != nil {
		t.Fatalf("parse seed records: %v", err)
	}
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Records: seed}, nil); err != nil {
		t.Fatalf("import seed records: %v", err)
	}

	incoming, err := parseJSONLDataRecords(bytes.NewBufferString(strings.Join([]string{
		`{"email":"live@example.test","db_id":2,"access_token":"v2-token"}`,
		`{"email":"live@example.test","db_id":1,"access_token":"v1-token"}`,
		`{"email":"dbnewer@example.test","db_id":49,"access_token":"older-token"}`,
		`{"email":"gap@example.test","db_id":59,"source_batch":"03.2026-05-11-LD260511PEX98T-30件-4.64元","mailbox_url":"https://mail.example.test/latest?pt=xyz"}`,
		`{"email":"fresh@example.test","db_id":7,"access_token":"fresh-token"}`,
	}, "\n")+"\n"), "incoming.jsonl")
	if err != nil {
		t.Fatalf("parse incoming records: %v", err)
	}
	var stats dataRecordsImportStats
	if err := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Dedupe: true, Records: incoming}, &stats); err != nil {
		t.Fatalf("import incoming records: %v", err)
	}
	if stats.Imported != 2 || stats.ReplacedExisting != 1 || stats.EnrichedExisting != 1 || stats.SkippedExisting != 1 || stats.DedupedWithinFile != 1 || stats.BatchesRegistered != 1 {
		t.Fatalf("dedupe stats = %#v", stats)
	}

	listed := listDataRecordsForTest(t, h)
	if listed.Total != 4 {
		t.Fatalf("total = %d, want 4", listed.Total)
	}
	rows := map[string]map[string]any{}
	for _, record := range listed.Records {
		var data map[string]any
		if err := json.Unmarshal(record.Data, &data); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		rows[data["email"].(string)] = data
	}
	live := rows["live@example.test"]
	if live["db_id"].(float64) != 2 || live["access_token"] != "v2-token" {
		t.Fatalf("live record = %#v, want db_id 2 with v2 token", live)
	}
	if live["quota"] != "57.63%" || live["nextTime"] != "8-8" || live["status"] != "ok" {
		t.Fatalf("live live-fields = %#v, want carried over", live)
	}
	if dbnewer := rows["dbnewer@example.test"]; dbnewer == nil || dbnewer["db_id"].(float64) != 50 || dbnewer["access_token"] != "current-token" {
		t.Fatalf("dbnewer record = %#v, want untouched db_id 50 with current token", dbnewer)
	}
	gap := rows["gap@example.test"]
	if gap["db_id"].(float64) != 60 || gap["source_batch"] != "03.2026-05-11-LD260511PEX98T-30件-4.64元" || gap["auth_state"] != "authorized" {
		t.Fatalf("gap record = %#v, want db_id 60 backfilled with batch and authorized", gap)
	}
	if rows["fresh@example.test"]["access_token"] != "fresh-token" {
		t.Fatalf("fresh record = %#v", rows["fresh@example.test"])
	}
}
