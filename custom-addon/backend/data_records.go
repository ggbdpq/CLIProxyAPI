package datarecords

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
)

const (
	dataRecordsDBDirName  = "data"
	dataRecordsDBFileName = "data-records.sqlite"
)

// Store is the local data-records backend kept in custom-addon.
type Store struct {
	ConfigFilePath string
	AuthDir        string
	ProxyURL       string
}

func New(configFilePath, authDir string) *Store {
	return &Store{ConfigFilePath: configFilePath, AuthDir: authDir}
}

func (s *Store) UpdateStatusByEmail(email, status string) (int, error) {
	return s.updateDataRecordQuotaStatusByEmail(email, "", "", status, "")
}

// UpdateHealthByEmail writes the probe-derived health state (plus optional
// quota/nextTime) onto every record matching the email.
func (s *Store) UpdateHealthByEmail(email, health, quota, nextTime string) (int, error) {
	return s.updateDataRecordQuotaStatusByEmail(email, nextTime, quota, "", health)
}

// UpdateTokensByEmail copies current credential tokens onto matching records.
func (s *Store) UpdateTokensByEmail(email string, tokens map[string]string) (int, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(tokens) == 0 {
		return 0, nil
	}
	pick := func(key string) string { return strings.TrimSpace(tokens[key]) }
	if pick("access_token") == "" && pick("refresh_token") == "" && pick("id_token") == "" && pick("expired") == "" && pick("last_refresh") == "" {
		return 0, nil
	}
	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		return 0, errPath
	}
	request := dataRecordsUpdateQuotaRequest{
		Action:       "update_quota",
		DBPath:       dbPath,
		Email:        email,
		AccessToken:  pick("access_token"),
		IDToken:      pick("id_token"),
		RefreshToken: pick("refresh_token"),
		Expired:      pick("expired"),
		LastRefresh:  pick("last_refresh"),
	}
	var payload dataRecordsUpdateNextTimeResponse
	if errUpdate := runDataRecordsSQLite(request, &payload); errUpdate != nil {
		return 0, errUpdate
	}
	return payload.Updated, nil
}

// DataRecord is a JSONL row persisted for the management data page.
type DataRecord struct {
	ID         int64          `json:"id"`
	SourceFile string         `json:"source_file"`
	LineNumber int            `json:"line_number"`
	Summary    map[string]any `json:"summary,omitempty"`
	Data       any            `json:"data"`
	ImportedAt string         `json:"imported_at"`
	TokenExp   *int64         `json:"token_exp,omitempty"`
}

type parsedDataRecord struct {
	SourceFile string `json:"source_file"`
	LineNumber int    `json:"line_number"`
	Data       string `json:"data"`
}

type dataRecordsImportRequest struct {
	Action  string             `json:"action"`
	DBPath  string             `json:"db_path"`
	Dedupe  bool               `json:"dedupe,omitempty"`
	Records []parsedDataRecord `json:"records"`
}

// dataRecordsImportStats reports what a single import actually wrote, so the
// caller can verify bulk imports (dedupe counters stay zero in blind mode).
type dataRecordsImportStats struct {
	Imported          int `json:"imported"`
	ReplacedExisting  int `json:"replaced_existing"`
	EnrichedExisting  int `json:"enriched_existing"`
	SkippedExisting   int `json:"skipped_existing"`
	DedupedWithinFile int `json:"deduped_within_file"`
	BatchesRegistered int `json:"batches_registered"`
}

type dataRecordsListRequest struct {
	Action    string `json:"action"`
	DBPath    string `json:"db_path"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
	Query     string `json:"query"`
	Lifecycle string `json:"lifecycle"`
	Health    string `json:"health"`
	AuthState string `json:"auth_state"`
	Batch     string `json:"batch"`
	Detected  bool   `json:"detected,omitempty"`
}

type dataRecordsDeleteRequest struct {
	Action string  `json:"action"`
	DBPath string  `json:"db_path"`
	IDs    []int64 `json:"ids"`
	All    bool    `json:"all"`
}

type dataRecordsUpdateQuotaRequest struct {
	Action       string `json:"action"`
	DBPath       string `json:"db_path"`
	Email        string `json:"email"`
	NextTime     string `json:"next_time"`
	Quota        string `json:"quota"`
	Status       string `json:"status"`
	Health       string `json:"health"`
	AccessToken  string `json:"access_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Expired      string `json:"expired,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`
}

type dataRecordsUpdateStateRequest struct {
	Action    string  `json:"action"`
	DBPath    string  `json:"db_path"`
	IDs       []int64 `json:"ids"`
	Lifecycle string  `json:"lifecycle"`
}

type dataRecordsUpdateBatchRequest struct {
	Action   string `json:"action"`
	DBPath   string `json:"db_path"`
	BatchKey string `json:"batch_key"`
	OrderURL string `json:"order_url"`
	Notes    string `json:"notes"`
}

type dataRecordsGenerateQuotaRequest struct {
	Action    string  `json:"action"`
	DBPath    string  `json:"db_path"`
	OutputDir string  `json:"output_dir"`
	IDs       []int64 `json:"ids"`
}

type dataRecordsDeployRequest struct {
	Action    string  `json:"action"`
	DBPath    string  `json:"db_path"`
	OutputDir string  `json:"output_dir"`
	Target    string  `json:"target"`
	IDs       []int64 `json:"ids"`
}

type dataRecordsRecycleRequest struct {
	Action    string  `json:"action"`
	DBPath    string  `json:"db_path"`
	OutputDir string  `json:"output_dir"`
	IDs       []int64 `json:"ids"`
}

type dataRecordsRegisterReauthRequest struct {
	Action          string             `json:"action"`
	DBPath          string             `json:"db_path"`
	Records         []parsedDataRecord `json:"records"`
	ImportUnmatched bool               `json:"import_unmatched"`
}

type dataRecordsExportRequest struct {
	Action string  `json:"action"`
	DBPath string  `json:"db_path"`
	IDs    []int64 `json:"ids"`
}

type dataRecordsConvertRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Content string `json:"content"`
}

type dataRecordsListResponse struct {
	Total   int          `json:"total"`
	Records []DataRecord `json:"records"`
}

type dataRecordsBatchesResponse struct {
	Total   int              `json:"total"`
	Batches []map[string]any `json:"batches"`
}

type dataRecordsDeleteResponse struct {
	Deleted int `json:"deleted"`
}

type dataRecordsUpdateNextTimeResponse struct {
	Updated int `json:"updated"`
}

type dataRecordsStatsResponse struct {
	Total      int            `json:"total"`
	Lifecycle  map[string]int `json:"lifecycle"`
	Health     map[string]int `json:"health"`
	AuthStates map[string]int `json:"auth_state"`
	Detected   int            `json:"detected"`
}

type dataRecordsGenerateQuotaResponse struct {
	Exported  int      `json:"exported"`
	OutputDir string   `json:"output_dir"`
	Files     []string `json:"files"`
}

type dataRecordsDeployResponse struct {
	Deployed  int      `json:"deployed"`
	OutputDir string   `json:"output_dir"`
	Target    string   `json:"target"`
	Files     []string `json:"files"`
}

type dataRecordsRecycleResponse struct {
	Recycled  int      `json:"recycled"`
	OutputDir string   `json:"output_dir"`
	Target    string   `json:"target"`
	Files     []string `json:"files"`
}

type dataRecordsRegisterReauthResponse struct {
	Total        int `json:"total"`
	Updated      int `json:"updated"`
	Imported     int `json:"imported"`
	Unmatched    int `json:"unmatched"`
	MissingEmail int `json:"missing_email"`
	MissingToken int `json:"missing_token"`
	Skipped      int `json:"skipped"`
}

type dataRecordsConvertResponse struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Filename  string `json:"filename"`
	Converted int    `json:"converted"`
	Content   string `json:"content"`
}

// ExportDataRecords returns selected records as JSONL, preserving the original import format.
func (s *Store) ExportDataRecords(c *gin.Context) {
	ids := parseIDsQuery(c)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data record ids selected"})
		return
	}

	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}

	request := dataRecordsExportRequest{Action: "export", DBPath: dbPath, IDs: ids}
	var stdout bytes.Buffer
	if errExport := runDataRecordsSQLite(request, &stdout); errExport != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to export data records: %v", errExport)})
		return
	}
	c.Header("Content-Type", "application/jsonl; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=data-records.jsonl")
	c.String(http.StatusOK, stdout.String())
}

func parseIDsQuery(c *gin.Context) []int64 {
	raw := strings.TrimSpace(c.Query("ids"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// ImportDataRecords imports JSONL rows into the local SQLite data database.
// With ?dedupe=1 it keeps the highest db_id per email and never overwrites an
// existing row whose db_id is newer; live fields (quota/nextTime/status) of a
// replaced row are carried over.
func (s *Store) ImportDataRecords(c *gin.Context) {
	reader, filename, closeFn, ok := dataRecordsUploadReader(c)
	if !ok {
		return
	}
	if closeFn != nil {
		defer closeFn()
	}

	records, errParse := parseJSONLDataRecords(reader, filename)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errParse.Error()})
		return
	}
	if len(records) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "jsonl file contains no records"})
		return
	}

	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	dedupe := c.Query("dedupe") == "1" || strings.EqualFold(strings.TrimSpace(c.Query("dedupe")), "true")
	var stats dataRecordsImportStats
	if errImport := runDataRecordsSQLite(dataRecordsImportRequest{Action: "import", DBPath: dbPath, Dedupe: dedupe, Records: records}, &stats); errImport != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to import data records: %v", errImport)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "imported": stats.Imported, "stats": stats})
}

// ListDataRecordBatches returns batch metadata with per-batch state aggregates.
// Batches seen only in records (no batches row yet) are included as well.
func (s *Store) ListDataRecordBatches(c *gin.Context) {
	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}

	var payload dataRecordsBatchesResponse
	if errList := runDataRecordsSQLite(dataRecordsListRequest{Action: "list_batches", DBPath: dbPath}, &payload); errList != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list batches: %v", errList)})
		return
	}
	if payload.Batches == nil {
		payload.Batches = []map[string]any{}
	}
	c.JSON(http.StatusOK, payload)
}

// ListDataRecords returns imported records from the local SQLite data database.
// Filters: lifecycle / health / auth_state / batch besides the free-text query.
func (s *Store) ListDataRecords(c *gin.Context) {
	limit := parsePositiveQueryInt(c, "limit", 50, 200)
	offset := parsePositiveQueryInt(c, "offset", 0, 0)
	query := strings.TrimSpace(c.Query("q"))

	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}

	request := dataRecordsListRequest{
		Action:    "list",
		DBPath:    dbPath,
		Limit:     limit,
		Offset:    offset,
		Query:     query,
		Lifecycle: strings.TrimSpace(c.Query("lifecycle")),
		Health:    strings.TrimSpace(c.Query("health")),
		AuthState: strings.TrimSpace(c.Query("auth_state")),
		Batch:     strings.TrimSpace(c.Query("batch")),
		Detected:  c.Query("detected") == "1",
	}
	var payload dataRecordsListResponse
	if errList := runDataRecordsSQLite(request, &payload); errList != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list data records: %v", errList)})
		return
	}
	if payload.Records == nil {
		payload.Records = []DataRecord{}
	}
	c.JSON(http.StatusOK, payload)
}

// StatsDataRecords returns record counts grouped by the three-state model.
func (s *Store) StatsDataRecords(c *gin.Context) {
	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	var payload dataRecordsStatsResponse
	if errStats := runDataRecordsSQLite(dataRecordsListRequest{Action: "stats", DBPath: dbPath}, &payload); errStats != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to collect stats: %v", errStats)})
		return
	}
	if payload.Lifecycle == nil {
		payload.Lifecycle = map[string]int{}
	}
	if payload.Health == nil {
		payload.Health = map[string]int{}
	}
	if payload.AuthStates == nil {
		payload.AuthStates = map[string]int{}
	}
	c.JSON(http.StatusOK, payload)
}

// openAITokenURL is the OAuth token endpoint used for refresh-token exchanges.
// Package-level so tests can point it at a stub server.
var openAITokenURL = "https://auth.openai.com/oauth/token"

// defaultCodexClientID is the public OAuth client used by every record in the inventory.
const defaultCodexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

type dataRecordsGetByIdRequest struct {
	Action string `json:"action"`
	DBPath string `json:"db_path"`
	ID     int64  `json:"id"`
}

type dataRecordsGetByIdResponse struct {
	Record *struct {
		ID   int64          `json:"id"`
		Data map[string]any `json:"data"`
	} `json:"record"`
}

type dataRecordsApplyRefreshRequest struct {
	Action       string `json:"action"`
	DBPath       string `json:"db_path"`
	ID           int64  `json:"id"`
	AccessToken  string `json:"access_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Failed       bool   `json:"failed,omitempty"`
}

type dataRecordsRefreshResponse struct {
	OK        bool   `json:"ok"`
	Email     string `json:"email"`
	ExpiresIn int64  `json:"expires_in"`
}

// RefreshDataRecordToken exchanges one record refresh_token for fresh tokens.
func (s *Store) RefreshDataRecordToken(c *gin.Context) {
	var req struct {
		ID int64 `json:"id"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid refresh request"})
		return
	}
	if req.ID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing record id"})
		return
	}
	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	var got dataRecordsGetByIdResponse
	if errGet := runDataRecordsSQLite(dataRecordsGetByIdRequest{Action: "get_by_id", DBPath: dbPath, ID: req.ID}, &got); errGet != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to load record: %v", errGet)})
		return
	}
	if got.Record == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
		return
	}
	data := got.Record.Data
	email, _ := data["email"].(string)
	refreshToken, _ := data["refresh_token"].(string)
	clientID, _ := data["client_id"].(string)
	if strings.TrimSpace(refreshToken) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "record has no refresh_token"})
		return
	}
	if strings.TrimSpace(clientID) == "" {
		clientID = defaultCodexClientID
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {strings.TrimSpace(refreshToken)},
		"scope":         {"openid profile email offline_access"},
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	httpReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, openAITokenURL, strings.NewReader(form.Encode()))
	if errReq != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("build refresh request: %v", errReq)})
		return
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := http.DefaultClient
	if strings.TrimSpace(s.ProxyURL) != "" {
		transport, _, errTransport := proxyutil.BuildHTTPTransport(s.ProxyURL)
		if errTransport == nil {
			client = &http.Client{Transport: transport}
		} else {
			log.WithError(errTransport).Debug("invalid proxy url for token refresh, falling back to direct")
		}
	}
	httpResp, errDo := client.Do(httpReq)
	if errDo != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("refresh request failed: %v", errDo)})
		return
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("refresh response close error")
		}
	}()
	body, errRead := io.ReadAll(httpResp.Body)
	if errRead != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read refresh response"})
		return
	}
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	_ = json.Unmarshal(body, &tokenResp)
	if httpResp.StatusCode != http.StatusOK || strings.TrimSpace(tokenResp.AccessToken) == "" {
		if strings.Contains(strings.ToLower(string(body)), "invalid_grant") || strings.Contains(strings.ToLower(tokenResp.Error), "invalid_grant") {
			if errMark := runDataRecordsSQLite(dataRecordsApplyRefreshRequest{Action: "apply_refresh", DBPath: dbPath, ID: req.ID, Failed: true}, nil); errMark != nil {
				log.WithError(errMark).Debug("failed to mark record needs_reauth")
			}
			c.JSON(http.StatusConflict, gin.H{"error": "refresh_token invalid (invalid_grant); needs re-authorization", "needs_reauth": true})
			return
		}
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 160 {
			snippet = snippet[:160]
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("token endpoint returned %d: %s", httpResp.StatusCode, snippet)})
		return
	}
	apply := dataRecordsApplyRefreshRequest{
		Action:       "apply_refresh",
		DBPath:       dbPath,
		ID:           req.ID,
		AccessToken:  tokenResp.AccessToken,
		IDToken:      tokenResp.IDToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
	}
	if errApply := runDataRecordsSQLite(apply, nil); errApply != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to apply refresh: %v", errApply)})
		return
	}
	expiresIn := tokenResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 28 * 24 * 3600
	}
	c.JSON(http.StatusOK, dataRecordsRefreshResponse{OK: true, Email: strings.TrimSpace(email), ExpiresIn: expiresIn})
}

// UpdateDataRecordState batch-sets the manual lifecycle field on selected records.
func (s *Store) UpdateDataRecordState(c *gin.Context) {
	var req struct {
		IDs       []int64 `json:"ids"`
		Lifecycle string  `json:"lifecycle"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid update request"})
		return
	}
	lifecycle := strings.TrimSpace(req.Lifecycle)
	switch lifecycle {
	case "unused", "in_use", "sold", "archived":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "lifecycle must be one of unused/in_use/sold/archived"})
		return
	}
	ids := uniquePositiveDataRecordIDs(req.IDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data record ids selected"})
		return
	}
	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	var payload dataRecordsUpdateNextTimeResponse
	if errUpdate := runDataRecordsSQLite(dataRecordsUpdateStateRequest{Action: "update_state", DBPath: dbPath, IDs: ids, Lifecycle: lifecycle}, &payload); errUpdate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update lifecycle: %v", errUpdate)})
		return
	}
	c.JSON(http.StatusOK, payload)
}

// UpdateDataRecordBatch edits batch ledger metadata (order link and notes).
func (s *Store) UpdateDataRecordBatch(c *gin.Context) {
	var req struct {
		BatchKey string `json:"batch_key"`
		OrderURL string `json:"order_url"`
		Notes    string `json:"notes"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid batch update request"})
		return
	}
	batchKey := strings.TrimSpace(req.BatchKey)
	if batchKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing batch_key"})
		return
	}
	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	var payload dataRecordsUpdateNextTimeResponse
	request := dataRecordsUpdateBatchRequest{Action: "update_batch", DBPath: dbPath, BatchKey: batchKey, OrderURL: strings.TrimSpace(req.OrderURL), Notes: strings.TrimSpace(req.Notes)}
	if errUpdate := runDataRecordsSQLite(request, &payload); errUpdate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update batch: %v", errUpdate)})
		return
	}
	c.JSON(http.StatusOK, payload)
}

// DeleteDataRecords deletes selected imported records from the local SQLite data database.
func (s *Store) DeleteDataRecords(c *gin.Context) {
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid delete request"})
		return
	}
	ids := uniquePositiveDataRecordIDs(req.IDs)
	if !req.All && len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data record ids selected"})
		return
	}

	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}

	var payload dataRecordsDeleteResponse
	if errDelete := runDataRecordsSQLite(dataRecordsDeleteRequest{Action: "delete", DBPath: dbPath, IDs: ids, All: req.All}, &payload); errDelete != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete data records: %v", errDelete)})
		return
	}
	c.JSON(http.StatusOK, payload)
}

// GenerateQuotaFiles keeps the legacy endpoint while using local deploy semantics.
func (s *Store) GenerateQuotaFiles(c *gin.Context) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid generate request"})
		return
	}
	ids := uniquePositiveDataRecordIDs(req.IDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data record ids selected"})
		return
	}

	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	outputDir := quotaFilesOutputDir(dbPath)
	var deployed dataRecordsDeployResponse
	request := dataRecordsDeployRequest{Action: "deploy", DBPath: dbPath, OutputDir: outputDir, Target: "local", IDs: ids}
	if errGenerate := runDataRecordsSQLite(request, &deployed); errGenerate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to generate quota files: %v", errGenerate)})
		return
	}
	payload := dataRecordsGenerateQuotaResponse{
		Exported:  deployed.Deployed,
		OutputDir: deployed.OutputDir,
		Files:     deployed.Files,
	}
	if payload.Files == nil {
		payload.Files = []string{}
	}
	c.JSON(http.StatusOK, payload)
}

func quotaFilesOutputDir(dbPath string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(dbPath)), ".cli-proxy-api")
}

// resolveDeployTarget maps a target key onto a whitelisted credential directory.
func (s *Store) resolveDeployTarget(target string) (string, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "local"
	}
	switch target {
	case "local":
		dbPath, errPath := s.dataRecordsDBPath()
		if errPath != nil {
			return "", "", errPath
		}
		return target, quotaFilesOutputDir(dbPath), nil
	case "official":
		if dir := strings.TrimSpace(os.Getenv("CPA_OFFICIAL_AUTH_DIR")); dir != "" {
			return target, dir, nil
		}
		return target, filepath.Join(`C:\Users\802165\CLIProxyAPI`, ".cli-proxy-api"), nil
	default:
		return "", "", fmt.Errorf("unknown deploy target %q (allowed: local, official)", target)
	}
}

// DeployDataRecords writes selected records into a whitelisted credential dir
// and marks them as deployed in the inventory.
func (s *Store) DeployDataRecords(c *gin.Context) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Target string  `json:"target"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deploy request"})
		return
	}
	ids := uniquePositiveDataRecordIDs(req.IDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data record ids selected"})
		return
	}
	target, dir, errTarget := s.resolveDeployTarget(req.Target)
	if errTarget != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errTarget.Error()})
		return
	}
	if errMkdir := os.MkdirAll(dir, 0o700); errMkdir != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to create deploy dir: %v", errMkdir)})
		return
	}

	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	var payload dataRecordsDeployResponse
	request := dataRecordsDeployRequest{Action: "deploy", DBPath: dbPath, OutputDir: dir, Target: target, IDs: ids}
	if errDeploy := runDataRecordsSQLite(request, &payload); errDeploy != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to deploy records: %v", errDeploy)})
		return
	}
	payload.Target = target
	if payload.Files == nil {
		payload.Files = []string{}
	}
	c.JSON(http.StatusOK, payload)
}

// RecycleDataRecords removes selected credential files from a whitelisted dir
// and returns the records to the unused inventory state.
func (s *Store) RecycleDataRecords(c *gin.Context) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Target string  `json:"target"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recycle request"})
		return
	}
	ids := uniquePositiveDataRecordIDs(req.IDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data record ids selected"})
		return
	}
	target, dir, errTarget := s.resolveDeployTarget(req.Target)
	if errTarget != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errTarget.Error()})
		return
	}

	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	var payload dataRecordsRecycleResponse
	request := dataRecordsRecycleRequest{Action: "recycle", DBPath: dbPath, OutputDir: dir, IDs: ids}
	if errRecycle := runDataRecordsSQLite(request, &payload); errRecycle != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to recycle records: %v", errRecycle)})
		return
	}
	payload.Target = target
	if payload.Files == nil {
		payload.Files = []string{}
	}
	c.JSON(http.StatusOK, payload)
}

// ConvertDataRecords converts between the small set of account formats used by the panel tools.
func (s *Store) ConvertDataRecords(c *gin.Context) {
	var req dataRecordsConvertRequest
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid convert request"})
		return
	}
	payload, errConvert := convertDataRecordsContent(req.From, req.To, req.Content, time.Now().UTC())
	if errConvert != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errConvert.Error()})
		return
	}
	c.JSON(http.StatusOK, payload)
}

// RegisterDataRecordReauth imports OAuth re-authorization output and writes fresh tokens by email.
func (s *Store) RegisterDataRecordReauth(c *gin.Context) {
	reader, filename, closeFn, ok := dataRecordsUploadReader(c)
	if !ok {
		return
	}
	if closeFn != nil {
		defer closeFn()
	}
	records, errParse := parseJSONLDataRecords(reader, filename)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errParse.Error()})
		return
	}
	if len(records) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "jsonl file contains no records"})
		return
	}
	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errPath.Error()})
		return
	}
	importUnmatched := true
	rawImportUnmatched := strings.TrimSpace(c.Query("import_unmatched"))
	if rawImportUnmatched == "0" || strings.EqualFold(rawImportUnmatched, "false") {
		importUnmatched = false
	}
	var payload dataRecordsRegisterReauthResponse
	request := dataRecordsRegisterReauthRequest{
		Action:          "register_reauth",
		DBPath:          dbPath,
		Records:         records,
		ImportUnmatched: importUnmatched,
	}
	if errApply := runDataRecordsSQLite(request, &payload); errApply != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to register reauth output: %v", errApply)})
		return
	}
	c.JSON(http.StatusOK, payload)
}

func uniquePositiveDataRecordIDs(input []int64) []int64 {
	seen := make(map[int64]struct{}, len(input))
	ids := make([]int64, 0, len(input))
	for _, id := range input {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (s *Store) updateDataRecordNextTimeByEmail(email string, nextTime string) (int, error) {
	return s.updateDataRecordQuotaStatusByEmail(email, nextTime, "", "", "")
}

func (s *Store) updateDataRecordQuotaByEmail(email string, nextTime string, quota string) (int, error) {
	return s.updateDataRecordQuotaStatusByEmail(email, nextTime, quota, "", "")
}

func (s *Store) updateDataRecordStatusByEmail(email string, status string) (int, error) {
	return s.updateDataRecordQuotaStatusByEmail(email, "", "", status, "")
}

func (s *Store) updateDataRecordQuotaStatusByEmail(email string, nextTime string, quota string, status string, health string) (int, error) {
	email = strings.TrimSpace(email)
	nextTime = strings.TrimSpace(nextTime)
	quota = strings.TrimSpace(quota)
	status = strings.TrimSpace(status)
	health = strings.TrimSpace(health)
	if email == "" || (nextTime == "" && quota == "" && status == "" && health == "") {
		return 0, nil
	}
	dbPath, errPath := s.dataRecordsDBPath()
	if errPath != nil {
		return 0, errPath
	}
	var payload dataRecordsUpdateNextTimeResponse
	request := dataRecordsUpdateQuotaRequest{Action: "update_quota", DBPath: dbPath, Email: email, NextTime: nextTime, Quota: quota, Status: status, Health: health}
	errUpdate := runDataRecordsSQLite(request, &payload)
	if errUpdate != nil {
		return 0, errUpdate
	}
	return payload.Updated, nil
}

func dataRecordsUploadReader(c *gin.Context) (io.Reader, string, func(), bool) {
	if c == nil || c.Request == nil {
		return nil, "", nil, false
	}

	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		file, header, errFile := c.Request.FormFile("file")
		if errFile != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing jsonl file"})
			return nil, "", nil, false
		}
		filename := "upload.jsonl"
		if header != nil && strings.TrimSpace(header.Filename) != "" {
			filename = filepath.Base(header.Filename)
		}
		return file, filename, func() {
			_ = file.Close()
		}, true
	}

	filename := strings.TrimSpace(c.Query("filename"))
	if filename == "" {
		filename = "upload.jsonl"
	}
	return c.Request.Body, filepath.Base(filename), nil, true
}

func parseJSONLDataRecords(reader io.Reader, filename string) ([]parsedDataRecord, error) {
	if reader == nil {
		return nil, errors.New("missing jsonl content")
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		filename = "upload.jsonl"
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	records := make([]parsedDataRecord, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if lineNumber == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" {
			continue
		}
		var raw json.RawMessage
		if errJSON := json.Unmarshal([]byte(line), &raw); errJSON != nil {
			return nil, fmt.Errorf("invalid jsonl at line %d", lineNumber)
		}
		canonical, errCanonical := canonicalJSON(raw)
		if errCanonical != nil {
			return nil, fmt.Errorf("invalid jsonl at line %d", lineNumber)
		}
		records = append(records, parsedDataRecord{SourceFile: filename, LineNumber: lineNumber, Data: canonical})
	}
	if errScan := scanner.Err(); errScan != nil {
		return nil, fmt.Errorf("read jsonl failed: %w", errScan)
	}
	return records, nil
}

func canonicalJSON(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	out, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		return "", errMarshal
	}
	return string(out), nil
}

func convertDataRecordsContent(from, to, content string, now time.Time) (dataRecordsConvertResponse, error) {
	from = normalizeDataRecordFormat(from)
	to = normalizeDataRecordFormat(to)
	if from == "" || to == "" {
		return dataRecordsConvertResponse{}, errors.New("format must be txt/cpa/sub2api")
	}
	if !supportedDataRecordConvertPair(from, to) {
		return dataRecordsConvertResponse{}, fmt.Errorf("unsupported conversion %s -> %s", from, to)
	}
	records, errParse := parseConvertibleDataRecords(content)
	if errParse != nil {
		return dataRecordsConvertResponse{}, errParse
	}
	if len(records) == 0 {
		return dataRecordsConvertResponse{}, errors.New("no account records found")
	}
	payload := dataRecordsConvertResponse{From: from, To: to, Converted: len(records)}
	switch to {
	case "cpa":
		lines := make([]string, 0, len(records))
		for _, record := range records {
			out, errMarshal := json.Marshal(toCPADataRecord(record))
			if errMarshal != nil {
				return dataRecordsConvertResponse{}, errMarshal
			}
			lines = append(lines, string(out))
		}
		payload.Filename = "cpa-records.jsonl"
		payload.Content = strings.Join(lines, "\n") + "\n"
	case "sub2api":
		bundle := map[string]any{
			"exported_at": now.UTC().Format(time.RFC3339),
			"proxies":     []any{},
			"accounts":    toSub2APIAccounts(records),
		}
		out, errMarshal := json.MarshalIndent(bundle, "", "  ")
		if errMarshal != nil {
			return dataRecordsConvertResponse{}, errMarshal
		}
		payload.Filename = "sub2api-accounts.json"
		payload.Content = string(out) + "\n"
	default:
		return dataRecordsConvertResponse{}, fmt.Errorf("unsupported target format %q", to)
	}
	return payload, nil
}

func normalizeDataRecordFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "txt", "text", "card", "card_txt":
		return "txt"
	case "cpa", "jsonl", "json":
		return "cpa"
	case "sub2api", "sub":
		return "sub2api"
	default:
		return ""
	}
}

func supportedDataRecordConvertPair(from, to string) bool {
	return (from == "txt" && to == "cpa") || (from == "cpa" && to == "sub2api") || (from == "sub2api" && to == "cpa")
}

func parseConvertibleDataRecords(content string) ([]map[string]any, error) {
	content = strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if content == "" {
		return nil, errors.New("missing content")
	}
	if records, ok := parseConvertibleJSONValue(content); ok {
		return records, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	records := make([]map[string]any, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if lineNumber == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || (!strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[")) {
			continue
		}
		lineRecords, ok := parseConvertibleJSONValue(line)
		if !ok {
			return nil, fmt.Errorf("invalid json at line %d", lineNumber)
		}
		records = append(records, lineRecords...)
	}
	if errScan := scanner.Err(); errScan != nil {
		return nil, fmt.Errorf("read content failed: %w", errScan)
	}
	return records, nil
}

func parseConvertibleJSONValue(text string) ([]map[string]any, bool) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if errDecode := decoder.Decode(&value); errDecode != nil {
		return nil, false
	}
	var extra any
	if errExtra := decoder.Decode(&extra); errExtra != io.EOF {
		return nil, false
	}
	records := dataRecordObjectsFromJSON(value)
	return records, len(records) > 0
}

func dataRecordObjectsFromJSON(value any) []map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		if accounts, ok := typed["accounts"].([]any); ok {
			records := make([]map[string]any, 0, len(accounts))
			for _, account := range accounts {
				if object, ok := account.(map[string]any); ok {
					records = append(records, sub2APIAccountToCPARecord(object))
				}
			}
			return records
		}
		return []map[string]any{typed}
	case []any:
		records := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				records = append(records, sub2APIAccountToCPARecord(object))
			}
		}
		return records
	default:
		return nil
	}
}

func sub2APIAccountToCPARecord(account map[string]any) map[string]any {
	record := map[string]any{}
	if credentials, ok := account["credentials"].(map[string]any); ok {
		copyNonEmptyDataRecordFields(record, credentials)
	}
	if extra, ok := account["extra"].(map[string]any); ok {
		copyMissingDataRecordFields(record, extra)
	}
	copyMissingDataRecordFields(record, account)
	if value := firstDataRecordString(record["expires_at"], account["expires_at"]); value != "" && firstDataRecordString(record["expired"]) == "" {
		record["expired"] = value
	}
	if email := firstDataRecordString(record["email"], nestedDataRecordString(account, "credentials", "email"), nestedDataRecordString(account, "extra", "email")); email != "" {
		record["email"] = email
	}
	if typ := firstDataRecordString(record["type"]); typ == "" || typ == "oauth" {
		record["type"] = "codex"
	}
	delete(record, "credentials")
	delete(record, "extra")
	delete(record, "accounts")
	return record
}

func toCPADataRecord(input map[string]any) map[string]any {
	record := sub2APIAccountToCPARecord(input)
	if firstDataRecordString(record["type"]) == "" {
		record["type"] = "codex"
	}
	if accountID := firstDataRecordString(record["account_id"], record["chatgpt_account_id"]); accountID != "" {
		record["account_id"] = accountID
	}
	return record
}

func toSub2APIAccounts(records []map[string]any) []map[string]any {
	accounts := make([]map[string]any, 0, len(records))
	for index, record := range records {
		cpa := toCPADataRecord(record)
		email := firstDataRecordString(cpa["email"], cpa["account_claims_email"], cpa["login_identity"], cpa["account_email"])
		name := email
		if name == "" {
			name = fmt.Sprintf("account-%d", index+1)
		}
		credentials := map[string]any{}
		for _, field := range []string{"access_token", "refresh_token", "id_token", "chatgpt_account_id", "chatgpt_user_id", "organization_id", "plan_type", "session_token"} {
			if value := firstDataRecordString(cpa[field]); value != "" {
				credentials[field] = value
			}
		}
		if email != "" {
			credentials["email"] = email
		}
		if value := firstDataRecordString(cpa["client_id"]); value != "" {
			credentials["client_id"] = value
		} else {
			credentials["client_id"] = defaultCodexClientID
		}
		if value := firstDataRecordString(cpa["expires_at"], cpa["expired"]); value != "" {
			credentials["expires_at"] = value
		}
		extra := map[string]any{}
		for _, field := range []string{"email", "auth_provider", "source", "source_batch", "reauth_at", "reset_expired_at", "mailbox_url", "mailapi_url"} {
			if value := firstDataRecordString(cpa[field]); value != "" {
				extra[field] = value
			}
		}
		accounts = append(accounts, map[string]any{
			"name":                  name,
			"platform":              "openai",
			"type":                  "oauth",
			"credentials":           credentials,
			"extra":                 extra,
			"concurrency":           10,
			"priority":              1,
			"rate_multiplier":       1,
			"auto_pause_on_expired": true,
		})
	}
	return accounts
}

func copyNonEmptyDataRecordFields(dst, src map[string]any) {
	for key, value := range src {
		if dataRecordValuePresent(value) {
			dst[key] = value
		}
	}
}

func copyMissingDataRecordFields(dst, src map[string]any) {
	for key, value := range src {
		if !dataRecordValuePresent(dst[key]) && dataRecordValuePresent(value) {
			dst[key] = value
		}
	}
}

func dataRecordValuePresent(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	default:
		return true
	}
}

func firstDataRecordString(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

func nestedDataRecordString(record map[string]any, objectKey, field string) string {
	if nested, ok := record[objectKey].(map[string]any); ok {
		return firstDataRecordString(nested[field])
	}
	return ""
}

func parsePositiveQueryInt(c *gin.Context, name string, fallback int, max int) int {
	if c == nil {
		return fallback
	}
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	if max > 0 && parsed > max {
		return max
	}
	return parsed
}

func (s *Store) dataRecordsDBPath() (string, error) {
	base := ""
	if s != nil {
		if strings.TrimSpace(s.ConfigFilePath) != "" {
			base = filepath.Dir(s.ConfigFilePath)
		}
		if base == "" && strings.TrimSpace(s.AuthDir) != "" {
			base = s.AuthDir
		}
	}
	if base == "" || base == "." {
		cwd, errCwd := os.Getwd()
		if errCwd != nil {
			return "", fmt.Errorf("failed to resolve data directory: %w", errCwd)
		}
		base = cwd
	}
	if strings.HasPrefix(base, "~/") || strings.HasPrefix(base, `~\`) {
		home, errHome := os.UserHomeDir()
		if errHome == nil && home != "" {
			base = filepath.Join(home, strings.TrimLeft(strings.TrimLeft(base, "~"), `/\`))
		}
	}
	dir := filepath.Join(base, dataRecordsDBDirName)
	if errMkdir := os.MkdirAll(dir, 0o700); errMkdir != nil {
		return "", fmt.Errorf("failed to create data directory: %w", errMkdir)
	}
	dbPath := filepath.Join(dir, dataRecordsDBFileName)
	if errMove := moveLegacyDataRecordsDB(base, dbPath); errMove != nil {
		return "", errMove
	}
	return dbPath, nil
}

func moveLegacyDataRecordsDB(base string, dbPath string) error {
	if _, errStat := os.Stat(dbPath); errStat == nil {
		return nil
	} else if !os.IsNotExist(errStat) {
		return fmt.Errorf("failed to check data records database: %w", errStat)
	}

	legacyPaths := []string{
		filepath.Join(base, dataRecordsDBFileName),
		filepath.Join(base, ".cli-proxy-api", dataRecordsDBFileName),
	}
	for _, legacyPath := range legacyPaths {
		if legacyPath == dbPath {
			continue
		}
		info, errStat := os.Stat(legacyPath)
		if os.IsNotExist(errStat) {
			continue
		}
		if errStat != nil {
			return fmt.Errorf("failed to check legacy data records database: %w", errStat)
		}
		if info.IsDir() {
			continue
		}
		if errRename := os.Rename(legacyPath, dbPath); errRename != nil {
			return fmt.Errorf("failed to move legacy data records database: %w", errRename)
		}
		return nil
	}
	return nil
}

func runDataRecordsSQLite(request any, response any) error {
	node, errNode := findSQLiteNode()
	if errNode != nil {
		return errNode
	}
	input, errMarshal := json.Marshal(request)
	if errMarshal != nil {
		return errMarshal
	}
	scriptFile, errScript := os.CreateTemp("", "cliproxy-data-records-*.cjs")
	if errScript != nil {
		return errScript
	}
	scriptPath := scriptFile.Name()
	defer func() {
		_ = os.Remove(scriptPath)
	}()
	if _, errWrite := scriptFile.WriteString(dataRecordsSQLiteScript); errWrite != nil {
		_ = scriptFile.Close()
		return errWrite
	}
	if errClose := scriptFile.Close(); errClose != nil {
		return errClose
	}
	cmd := exec.Command(node, "--no-warnings", scriptPath)
	cmd.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if errRun := cmd.Run(); errRun != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = errRun.Error()
		}
		return errors.New(message)
	}
	if response == nil {
		return nil
	}
	if buf, ok := response.(*bytes.Buffer); ok {
		*buf = stdout
		return nil
	}
	if errDecode := json.Unmarshal(stdout.Bytes(), response); errDecode != nil {
		return fmt.Errorf("decode sqlite response failed: %w", errDecode)
	}
	return nil
}

func findSQLiteNode() (string, error) {
	candidates := []string{}
	if env := strings.TrimSpace(os.Getenv("CLIPROXY_SQLITE_NODE")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates, "node")
	if home, errHome := os.UserHomeDir(); errHome == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".cache", "codex-runtimes", "codex-primary-runtime", "dependencies", "node", "bin", "node.exe"))
	}
	for _, candidate := range candidates {
		if strings.ContainsAny(candidate, `/\`) {
			if info, errStat := os.Stat(candidate); errStat == nil && !info.IsDir() && hasBuiltInSQLiteNode(candidate) {
				return candidate, nil
			}
			continue
		}
		if path, errLook := exec.LookPath(candidate); errLook == nil && hasBuiltInSQLiteNode(path) {
			return path, nil
		}
	}
	return "", errors.New("node 24 with built-in sqlite module was not found")
}

// SQLiteAvailable reports whether a node runtime with the built-in sqlite module can be located.
func SQLiteAvailable() bool {
	_, errNode := findSQLiteNode()
	return errNode == nil
}

func hasBuiltInSQLiteNode(candidate string) bool {
	cmd := exec.Command(candidate, "--no-warnings", "-e", `const sqlite = require("node:sqlite"); if (typeof sqlite.DatabaseSync !== "function") process.exit(1);`)
	return cmd.Run() == nil
}

const dataRecordsSQLiteScript = `
const fs = require("node:fs");
const path = require("node:path");
const { DatabaseSync } = require("node:sqlite");

const SCHEMA = [
  "CREATE TABLE IF NOT EXISTS data_records (",
  "  id INTEGER PRIMARY KEY AUTOINCREMENT,",
  "  source_file TEXT NOT NULL,",
  "  line_number INTEGER NOT NULL,",
  "  record_json TEXT NOT NULL,",
  "  imported_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))",
  ");",
  "CREATE INDEX IF NOT EXISTS idx_data_records_imported_at ON data_records(imported_at DESC, id DESC);",
  "CREATE TABLE IF NOT EXISTS batches (",
  "  id INTEGER PRIMARY KEY AUTOINCREMENT,",
  "  batch_key TEXT NOT NULL UNIQUE,",
  "  seq INTEGER,",
  "  batch_date TEXT,",
  "  order_id TEXT,",
  "  quantity INTEGER,",
  "  total_cost TEXT,",
  "  order_url TEXT NOT NULL DEFAULT '',",
  "  notes TEXT NOT NULL DEFAULT '',",
  "  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))",
  ");",
].join("\n");

const SUMMARY_FIELDS = [
  "db_id",
  "quota",
  "platform",
  "email",
  "login_identity",
  "phone",
  "status",
  "source",
  "chatgpt_account_id",
  "chatgpt_user_id",
  "organization_id",
  "project_id",
  "workspace_id",
  "created_at",
  "last_used",
];

function summarizeRecord(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const summary = {};
  for (const field of SUMMARY_FIELDS) {
    if (value[field] !== undefined && value[field] !== null && value[field] !== "") {
      summary[field] = value[field];
    }
  }
  const mailbox = value.mailbox;
  if (mailbox && typeof mailbox === "object" && !Array.isArray(mailbox)) {
    if (mailbox.provider !== undefined && mailbox.provider !== null && mailbox.provider !== "") {
      summary.mailbox_provider = mailbox.provider;
    }
    if (mailbox.enabled !== undefined && mailbox.enabled !== null) {
      summary.mailbox_enabled = mailbox.enabled;
    }
  }
  return summary;
}

function connect(dbPath) {
  fs.mkdirSync(path.dirname(dbPath), { recursive: true });
  const db = new DatabaseSync(dbPath);
  db.exec(SCHEMA);
  db.function("cpa_next_time_sort_key", { deterministic: true }, parseNextTimeSortKey);
  return db;
}

// parseBatchMeta extracts structured fields from card-key batch names like
// "02.2026-05-08-LD260508O5INF3-10件-4.12元". Unmatched keys are still kept,
// just without parsed metadata.
function parseBatchMeta(key) {
  const text = String(key || "").trim();
  const match = /^(\d{1,4})\.(\d{4}-\d{2}-\d{2})-([A-Za-z0-9]+)-(\d+)件-([0-9]+(?:\.[0-9]+)?)元$/.exec(text);
  if (!match) {
    return { batch_key: text, seq: null, batch_date: "", order_id: "", quantity: null, total_cost: "" };
  }
  return {
    batch_key: text,
    seq: Number.parseInt(match[1], 10),
    batch_date: match[2],
    order_id: match[3],
    quantity: Number.parseInt(match[4], 10),
    total_cost: match[5],
  };
}

function collectBatchKeys(records) {
  const keys = new Set();
  for (const record of records) {
    let data;
    try { data = JSON.parse(record.data || "null"); } catch (_) { data = null; }
    const key = data && typeof data === "object" && !Array.isArray(data) ? String(data.source_batch || "").trim() : "";
    if (key) keys.add(key);
  }
  return Array.from(keys);
}

function registerBatches(db, records) {
  const keys = collectBatchKeys(records);
  if (!keys.length) return 0;
  const insert = db.prepare("INSERT INTO batches(batch_key,seq,batch_date,order_id,quantity,total_cost) VALUES(?,?,?,?,?,?) ON CONFLICT(batch_key) DO NOTHING");
  let registered = 0;
  db.exec("BEGIN");
  try {
    for (const key of keys) {
      const meta = parseBatchMeta(key);
      const result = insert.run(meta.batch_key, meta.seq, meta.batch_date, meta.order_id, meta.quantity, meta.total_cost);
      registered += Number(result.changes || 0);
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  return registered;
}

// deriveAuthState recomputes the auth_state value from the mailbox fields.
function deriveAuthState(data) {
  const mailboxApi = String((data.mailbox_url || (data.mailbox && data.mailbox.mailapi_url)) || "").trim();
  return mailboxApi ? "authorized" : "missing_mailapi_url";
}

// applyDefaultStates fills the three-state model on first sight of a record:
// lifecycle (business, manual), health (probe-written), auth_state (derived).
// Values already present on the record are never overwritten.
function applyDefaultStates(data) {
  if (!data || typeof data !== "object" || Array.isArray(data)) return data;
  if (data.lifecycle === undefined || data.lifecycle === null || data.lifecycle === "") data.lifecycle = "unused";
  if (data.health === undefined || data.health === null || data.health === "") data.health = "unknown";
  if (data.auth_state === undefined || data.auth_state === null || data.auth_state === "") {
    data.auth_state = deriveAuthState(data);
  }
  return data;
}

// refreshDerivedAuthState recomputes auth_state when it still holds one of the
// derived values (or is unset), so a backfilled mailbox field corrects a stale
// derivation. Operational states such as "needs_reauth" are never clobbered.
function refreshDerivedAuthState(data) {
  if (!data || typeof data !== "object" || Array.isArray(data)) return;
  const current = data.auth_state;
  if (current === undefined || current === null || current === "" || current === "authorized" || current === "missing_mailapi_url") {
    data.auth_state = deriveAuthState(data);
  }
}

function withDefaultStates(rawData) {
  let data;
  try { data = JSON.parse(rawData || "null"); } catch (_) { return rawData || "null"; }
  if (!data || typeof data !== "object" || Array.isArray(data)) return rawData || "null";
  return JSON.stringify(applyDefaultStates(data));
}

const LIVE_FIELDS_CARRIED_ON_REPLACE = ["quota", "nextTime", "status"];

function runImport(db, req) {
  const records = Array.isArray(req.records) ? req.records : [];
  if (req.dedupe) {
    process.stdout.write(JSON.stringify(runImportDedupe(db, records)));
    return;
  }
  const insert = db.prepare("INSERT INTO data_records(source_file,line_number,record_json) VALUES(?,?,?)");
  db.exec("BEGIN");
  try {
    for (const record of records) {
      insert.run(record.source_file || "upload.jsonl", Number.parseInt(record.line_number || 0, 10), withDefaultStates(record.data));
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  const batchesRegistered = registerBatches(db, records);
  process.stdout.write(JSON.stringify({ imported: records.length, replaced_existing: 0, skipped_existing: 0, deduped_within_file: 0, batches_registered: batchesRegistered }));
}

// runImportDedupe dedupes by email. Within the file the highest db_id wins.
// Against the database: a file record with a strictly higher db_id replaces the
// row (carrying over its live fields quota/nextTime/status); otherwise the
// database row wins and only fields the row is missing (e.g. source_batch,
// mailbox_url) are backfilled from the file record.
function runImportDedupe(db, records) {
  const byEmail = new Map();
  const plain = [];
  let dedupedWithinFile = 0;
  for (const record of records) {
    let data;
    try { data = JSON.parse(record.data || "null"); } catch (_) { data = null; }
    const email = emailFromRecord(data);
    if (!email) {
      plain.push(record);
      continue;
    }
    const dbId = Number.parseInt(data && data.db_id, 10) || 0;
    const prev = byEmail.get(email);
    if (prev) {
      dedupedWithinFile += 1;
      if (dbId <= prev.dbId) continue;
    }
    byEmail.set(email, { record, data, dbId });
  }

  const existing = new Map();
  const rows = db.prepare("SELECT id, record_json FROM data_records").all();
  for (const row of rows) {
    let data;
    try { data = JSON.parse(row.record_json); } catch (_) { data = null; }
    const email = emailFromRecord(data);
    if (!email) continue;
    const dbId = Number.parseInt(data && data.db_id, 10) || 0;
    const prev = existing.get(email);
    if (!prev || dbId > prev.dbId) existing.set(email, { id: row.id, data: data || {}, dbId });
  }

  const insert = db.prepare("INSERT INTO data_records(source_file,line_number,record_json) VALUES(?,?,?)");
  const update = db.prepare("UPDATE data_records SET record_json = ? WHERE id = ?");
  const survivors = [];
  let importedNew = 0;
  let replacedExisting = 0;
  let enrichedExisting = 0;
  let skippedExisting = 0;
  db.exec("BEGIN");
  try {
    for (const record of plain) {
      insert.run(record.source_file || "upload.jsonl", Number.parseInt(record.line_number || 0, 10), withDefaultStates(record.data));
      survivors.push(record);
      importedNew += 1;
    }
    for (const [email, entry] of byEmail.entries()) {
      const prev = existing.get(email);
      if (!prev) {
        insert.run(entry.record.source_file || "upload.jsonl", Number.parseInt(entry.record.line_number || 0, 10), withDefaultStates(entry.record.data));
        survivors.push(entry.record);
        importedNew += 1;
        continue;
      }
      if (entry.dbId > prev.dbId) {
        const merged = { ...entry.data };
        for (const field of LIVE_FIELDS_CARRIED_ON_REPLACE) {
          if (prev.data[field] !== undefined && prev.data[field] !== null && prev.data[field] !== "") merged[field] = prev.data[field];
        }
        update.run(JSON.stringify(applyDefaultStates(merged)), prev.id);
        survivors.push({ source_file: entry.record.source_file, line_number: entry.record.line_number, data: JSON.stringify(merged) });
        replacedExisting += 1;
        continue;
      }
      const merged = { ...prev.data };
      let changed = false;
      for (const [field, value] of Object.entries(entry.data)) {
        if (merged[field] === undefined || merged[field] === null || merged[field] === "") {
          merged[field] = value;
          changed = true;
        }
      }
      if (changed) {
        refreshDerivedAuthState(merged);
        update.run(JSON.stringify(applyDefaultStates(merged)), prev.id);
        survivors.push({ source_file: entry.record.source_file, line_number: entry.record.line_number, data: JSON.stringify(merged) });
        enrichedExisting += 1;
      } else {
        skippedExisting += 1;
      }
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  const batchesRegistered = registerBatches(db, survivors);
  return { imported: importedNew + replacedExisting, replaced_existing: replacedExisting, enriched_existing: enrichedExisting, skipped_existing: skippedExisting, deduped_within_file: dedupedWithinFile, batches_registered: batchesRegistered };
}

function runGetById(db, req) {
  const id = Number.parseInt(req.id, 10);
  if (!Number.isSafeInteger(id) || id <= 0) throw new Error("invalid id");
  const row = db.prepare("SELECT id, record_json FROM data_records WHERE id = ?").get(id);
  if (!row) {
    process.stdout.write(JSON.stringify({ record: null }));
    return;
  }
  process.stdout.write(JSON.stringify({ record: { id: row.id, data: JSON.parse(row.record_json) } }));
}

function runApplyRefresh(db, req) {
  const id = Number.parseInt(req.id, 10);
  if (!Number.isSafeInteger(id) || id <= 0) throw new Error("invalid id");
  const row = db.prepare("SELECT record_json FROM data_records WHERE id = ?").get(id);
  if (!row) throw new Error("record not found");
  const data = JSON.parse(row.record_json);
  if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error("record is not an object");
  if (req.failed) {
    data.auth_state = "needs_reauth";
    db.prepare("UPDATE data_records SET record_json = ? WHERE id = ?").run(JSON.stringify(data), id);
    process.stdout.write(JSON.stringify({ updated: 1 }));
    return;
  }
  if (req.access_token) data.access_token = String(req.access_token);
  if (req.id_token) data.id_token = String(req.id_token);
  if (req.refresh_token) data.refresh_token = String(req.refresh_token);
  if (req.expires_in) data.expired = new Date(Date.now() + Number(req.expires_in) * 1000).toISOString();
  data.last_refresh = new Date().toISOString();
  data.health = "unknown";
  delete data.quota;
  delete data.nextTime;
  db.prepare("UPDATE data_records SET record_json = ? WHERE id = ?").run(JSON.stringify(data), id);
  process.stdout.write(JSON.stringify({ updated: 1 }));
}

function bumpCount(target, key, n) {
  const name = key === null || key === undefined || key === "" ? "unset" : String(key);
  target[name] = (target[name] || 0) + n;
}

// runListBatches aggregates records per source_batch. Batches that exist only
// as record fields (no batches row) are listed too, so the ledger self-heals.
function runListBatches(db) {
  const meta = new Map();
  const batchRows = db.prepare("SELECT batch_key, seq, batch_date, order_id, quantity, total_cost, order_url, notes FROM batches").all();
  for (const row of batchRows) meta.set(row.batch_key, row);

  const statRows = db.prepare("SELECT json_extract(record_json,'$.source_batch') AS batch_key, json_extract(record_json,'$.lifecycle') AS lifecycle, json_extract(record_json,'$.health') AS health, json_extract(record_json,'$.auth_state') AS auth_state, COUNT(*) AS n FROM data_records GROUP BY batch_key, lifecycle, health, auth_state").all();
  const groups = new Map();
  for (const row of statRows) {
    const key = String(row.batch_key || "").trim();
    if (!key) continue;
    if (!groups.has(key)) groups.set(key, { record_count: 0, lifecycle: {}, health: {}, auth_state: {} });
    const group = groups.get(key);
    group.record_count += row.n;
    bumpCount(group.lifecycle, row.lifecycle, row.n);
    bumpCount(group.health, row.health, row.n);
    bumpCount(group.auth_state, row.auth_state, row.n);
  }

  const keys = new Set(Array.from(meta.keys()).concat(Array.from(groups.keys())));
  const batches = [];
  for (const key of keys) {
    const base = meta.get(key) || { batch_key: key, seq: null, batch_date: "", order_id: "", quantity: null, total_cost: "", order_url: "", notes: "" };
    const stats = groups.get(key) || { record_count: 0, lifecycle: {}, health: {}, auth_state: {} };
    batches.push({ ...base, ...stats });
  }
  batches.sort((a, b) => {
    const seqA = a.seq === null || a.seq === undefined ? Number.MAX_SAFE_INTEGER : a.seq;
    const seqB = b.seq === null || b.seq === undefined ? Number.MAX_SAFE_INTEGER : b.seq;
    return seqA - seqB || String(a.batch_key).localeCompare(String(b.batch_key));
  });
  process.stdout.write(JSON.stringify({ total: batches.length, batches }));
}

function parseNextTimeSortKey(value) {
  const text = typeof value === "string" ? value.trim() : "";
  const match = /^(\d{1,2})-(\d{1,2})(?:\s+(\d{1,2}):(\d{1,2}))?$/.exec(text);
  if (!match) return null;
  const month = Number.parseInt(match[1], 10);
  const day = Number.parseInt(match[2], 10);
  const hour = match[3] === undefined ? 0 : Number.parseInt(match[3], 10);
  const minute = match[4] === undefined ? 0 : Number.parseInt(match[4], 10);
  if (month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59) return null;
  return (((month * 32) + day) * 24 + hour) * 60 + minute;
}

function tokenExpOf(data) {
  if (!data || typeof data !== "object") return null;
  if (data.expired) {
    const parsed = Date.parse(data.expired);
    if (!Number.isNaN(parsed)) return Math.floor(parsed / 1000);
  }
  const token = typeof data.access_token === "string" ? data.access_token : "";
  const parts = token.split(".");
  if (parts.length !== 3) return null;
  try {
    const payload = JSON.parse(Buffer.from(parts[1].replace(/-/g, "+").replace(/_/g, "/"), "base64").toString("utf8"));
    if (payload && typeof payload.exp === "number") return payload.exp;
  } catch (_) {}
  return null;
}

function runList(db, req) {
  const limit = Math.max(0, Number.parseInt(req.limit || 50, 10));
  const offset = Math.max(0, Number.parseInt(req.offset || 0, 10));
  const query = typeof req.query === "string" ? req.query.trim() : "";
  let where = "";
  let args = [];
  if (query) {
    where = " WHERE coalesce(json_extract(record_json, '$.email'), '') LIKE ? OR coalesce(json_extract(record_json, '$.account_claims_email'), '') LIKE ? OR coalesce(json_extract(record_json, '$.status'), '') LIKE ? OR coalesce(json_extract(record_json, '$.nextTime'), '') LIKE ?";
    const like = "%" + query + "%";
    args = [like, like, like, like];
  }
  const filterClauses = [];
  const filterArgs = [];
  if (req.detected) {
    filterClauses.push("coalesce(json_extract(record_json, '$.quota'), '') <> '' AND coalesce(json_extract(record_json, '$.nextTime'), '') <> ''");
  }
  const fieldFilters = [["lifecycle", req.lifecycle], ["health", req.health], ["auth_state", req.auth_state], ["source_batch", req.batch]];
  for (const [field, value] of fieldFilters) {
    const text = typeof value === "string" ? value.trim() : "";
    if (!text) continue;
    if (field === "health" && text === "abnormal") {
      filterClauses.push("json_extract(record_json, '$.health') IS NOT NULL AND json_extract(record_json, '$.health') NOT IN ('ok','unknown')");
      continue;
    }
    filterClauses.push("json_extract(record_json, '$." + field + "') = ?");
    filterArgs.push(text);
  }
  if (filterClauses.length) {
    where += (where ? " AND " : " WHERE ") + filterClauses.join(" AND ");
    args = args.concat(filterArgs);
  }
  const total = db.prepare("SELECT COUNT(*) AS total FROM data_records" + where).get(...args).total;
  const rows = db.prepare("SELECT id, source_file, line_number, record_json, imported_at, cpa_next_time_sort_key(json_extract(record_json, '$.nextTime')) AS next_time_sort FROM data_records" + where + " ORDER BY CASE WHEN next_time_sort IS NULL THEN 1 ELSE 0 END, next_time_sort ASC, id DESC LIMIT ? OFFSET ?").all(...args, limit, offset);
  const payload = {
    total,
    records: rows.map((row) => {
      const data = JSON.parse(row.record_json);
      return {
        id: row.id,
        source_file: row.source_file,
        line_number: row.line_number,
        summary: summarizeRecord(data),
        data,
        imported_at: row.imported_at,
        token_exp: tokenExpOf(data),
      };
    }),
  };
  process.stdout.write(JSON.stringify(payload));
}

function runStats(db) {
  const rows = db.prepare("SELECT json_extract(record_json, '$.lifecycle') AS lifecycle, json_extract(record_json, '$.health') AS health, json_extract(record_json, '$.auth_state') AS auth_state, coalesce(json_extract(record_json, '$.quota'), '') <> '' AND coalesce(json_extract(record_json, '$.nextTime'), '') <> '' AS detected, COUNT(*) AS n FROM data_records GROUP BY lifecycle, health, auth_state, detected").all();
  const payload = { total: 0, lifecycle: {}, health: {}, auth_state: {}, detected: 0 };
  for (const row of rows) {
    payload.total += row.n;
    bumpCount(payload.lifecycle, row.lifecycle, row.n);
    bumpCount(payload.health, row.health, row.n);
    bumpCount(payload.auth_state, row.auth_state, row.n);
    if (row.detected) payload.detected += row.n;
  }
  process.stdout.write(JSON.stringify(payload));
}

const LIFECYCLE_VALUES = ["unused", "in_use", "sold", "archived"];

function runUpdateState(db, req) {
  const lifecycle = typeof req.lifecycle === "string" ? req.lifecycle.trim() : "";
  if (LIFECYCLE_VALUES.indexOf(lifecycle) === -1) throw new Error("invalid lifecycle value");
  const ids = Array.isArray(req.ids) ? req.ids.map((id) => Number.parseInt(id, 10)).filter((id) => Number.isSafeInteger(id) && id > 0) : [];
  const update = db.prepare("UPDATE data_records SET record_json = ? WHERE id = ?");
  const select = db.prepare("SELECT record_json FROM data_records WHERE id = ?");
  let updated = 0;
  db.exec("BEGIN");
  try {
    for (const id of ids) {
      const row = select.get(id);
      if (!row) continue;
      const data = JSON.parse(row.record_json);
      if (!data || typeof data !== "object" || Array.isArray(data)) continue;
      if (data.lifecycle === lifecycle) continue;
      data.lifecycle = lifecycle;
      update.run(JSON.stringify(data), id);
      updated += 1;
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  process.stdout.write(JSON.stringify({ updated }));
}

function runUpdateBatch(db, req) {
  const key = typeof req.batch_key === "string" ? req.batch_key.trim() : "";
  if (!key) throw new Error("missing batch_key");
  const orderUrl = typeof req.order_url === "string" ? req.order_url.trim() : "";
  const notes = typeof req.notes === "string" ? req.notes.trim() : "";
  const result = db.prepare("INSERT INTO batches(batch_key,order_url,notes) VALUES(?,?,?) ON CONFLICT(batch_key) DO UPDATE SET order_url = excluded.order_url, notes = excluded.notes").run(key, orderUrl, notes);
  process.stdout.write(JSON.stringify({ updated: Number(result.changes || 0) }));
}

function emailFromRecord(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return "";
  const candidates = [
    value.email,
    value.account_claims_email,
    value.login_identity,
    value.account_email,
    value.chatgpt_email,
  ];
  const accountClaims = value.account_claims;
  if (accountClaims && typeof accountClaims === "object" && !Array.isArray(accountClaims)) {
    candidates.push(accountClaims.email);
  }
  const account = value.account;
  if (account && typeof account === "object" && !Array.isArray(account)) {
    candidates.push(account.email);
  }
  const user = value.user;
  if (user && typeof user === "object" && !Array.isArray(user)) {
    candidates.push(user.email);
  }
  const credentials = value.credentials;
  if (credentials && typeof credentials === "object" && !Array.isArray(credentials)) {
    candidates.push(credentials.email);
  }
  const extra = value.extra;
  if (extra && typeof extra === "object" && !Array.isArray(extra)) {
    candidates.push(extra.email);
  }
  for (const candidate of candidates) {
    if (typeof candidate === "string" && candidate.trim()) return candidate.trim().toLowerCase();
  }
  return "";
}

function runUpdateQuota(db, req) {
  const email = typeof req.email === "string" ? req.email.trim().toLowerCase() : "";
  const nextTime = typeof req.next_time === "string" ? req.next_time.trim() : "";
  const quota = typeof req.quota === "string" ? req.quota.trim() : "";
  const status = typeof req.status === "string" ? req.status.trim() : "";
  const health = typeof req.health === "string" ? req.health.trim() : "";
  const tokens = {};
  for (const field of ["access_token", "id_token", "refresh_token", "expired", "last_refresh"]) {
    if (typeof req[field] === "string" && req[field].trim()) tokens[field] = req[field].trim();
  }
  if (!email || (!nextTime && !quota && !status && !health && Object.keys(tokens).length === 0)) {
    process.stdout.write(JSON.stringify({ updated: 0 }));
    return;
  }
  const rows = db.prepare("SELECT id, record_json FROM data_records").all();
  const update = db.prepare("UPDATE data_records SET record_json = ? WHERE id = ?");
  let updated = 0;
  db.exec("BEGIN");
  try {
    for (const row of rows) {
      const data = JSON.parse(row.record_json);
      if (emailFromRecord(data) !== email) continue;
      if (!data || typeof data !== "object" || Array.isArray(data)) continue;
      if (nextTime) data.nextTime = nextTime;
      if (quota) data.quota = quota;
      if (status) data.status = status;
      if (health) data.health = health;
      for (const [field, value] of Object.entries(tokens)) data[field] = value;
      update.run(JSON.stringify(data), row.id);
      updated += 1;
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  process.stdout.write(JSON.stringify({ updated }));
}

function runUpdateNextTime(db, req) {
  runUpdateQuota(db, req);
}

function reauthRecordData(raw) {
  let data;
  try { data = JSON.parse(raw || "null"); } catch (_) { return null; }
  if (!data || typeof data !== "object" || Array.isArray(data)) return null;
  return data;
}

function hasReauthToken(data) {
  return !!firstString(data.access_token, data.refresh_token, data.id_token);
}

function applyReauthFields(target, incoming, now) {
  for (const field of ["access_token", "refresh_token", "id_token", "expired", "last_refresh", "account_id", "chatgpt_account_id", "chatgpt_user_id", "organization_id", "client_id", "session_token", "plan_type", "mailapi_url", "mailbox_url"]) {
    if (incoming[field] !== undefined && incoming[field] !== null && String(incoming[field]).trim() !== "") target[field] = incoming[field];
  }
  target.auth_state = "authorized";
  target.health = "unknown";
  target.reauth_at = firstString(incoming.reauth_at) || now;
  if (firstString(incoming.expired)) target.reset_expired_at = firstString(incoming.expired);
  delete target.quota;
  delete target.nextTime;
  return target;
}

function runRegisterReauth(db, req) {
  const records = Array.isArray(req.records) ? req.records : [];
  const importUnmatched = req.import_unmatched !== false;
  const rows = db.prepare("SELECT id, record_json FROM data_records").all();
  const byEmail = new Map();
  for (const row of rows) {
    const data = reauthRecordData(row.record_json);
    const email = emailFromRecord(data);
    if (!email) continue;
    if (!byEmail.has(email)) byEmail.set(email, []);
    byEmail.get(email).push({ id: row.id, data });
  }
  const update = db.prepare("UPDATE data_records SET record_json = ? WHERE id = ?");
  const insert = db.prepare("INSERT INTO data_records(source_file,line_number,record_json) VALUES(?,?,?)");
  const now = new Date().toISOString();
  const inserted = [];
  const stats = { total: records.length, updated: 0, imported: 0, unmatched: 0, missing_email: 0, missing_token: 0, skipped: 0 };
  db.exec("BEGIN");
  try {
    for (const record of records) {
      const incoming = reauthRecordData(record.data);
      if (!incoming) {
        stats.skipped += 1;
        continue;
      }
      const email = emailFromRecord(incoming);
      if (!email) {
        stats.missing_email += 1;
        continue;
      }
      if (!hasReauthToken(incoming)) {
        stats.missing_token += 1;
        continue;
      }
      incoming.email = email;
      const matched = byEmail.get(email) || [];
      if (matched.length) {
        for (const row of matched) {
          update.run(JSON.stringify(applyReauthFields(row.data, incoming, now)), row.id);
          stats.updated += 1;
        }
        continue;
      }
      stats.unmatched += 1;
      if (!importUnmatched) continue;
      const fresh = applyDefaultStates(applyReauthFields({ ...incoming }, incoming, now));
      insert.run(record.source_file || "reauth.jsonl", Number.parseInt(record.line_number || 0, 10), JSON.stringify(fresh));
      inserted.push({ source_file: record.source_file, line_number: record.line_number, data: JSON.stringify(fresh) });
      stats.imported += 1;
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  registerBatches(db, inserted);
  process.stdout.write(JSON.stringify(stats));
}

function runDelete(db, req) {
  if (req.all) {
    const result = db.prepare("DELETE FROM data_records").run();
    process.stdout.write(JSON.stringify({ deleted: Number(result.changes || 0) }));
    return;
  }
  const ids = Array.isArray(req.ids) ? req.ids.map((id) => Number.parseInt(id, 10)).filter((id) => Number.isSafeInteger(id) && id > 0) : [];
  if (!ids.length) {
    process.stdout.write(JSON.stringify({ deleted: 0 }));
    return;
  }
  const statement = db.prepare("DELETE FROM data_records WHERE id = ?");
  let deleted = 0;
  db.exec("BEGIN");
  try {
    for (const id of ids) {
      const result = statement.run(id);
      deleted += Number(result.changes || 0);
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  process.stdout.write(JSON.stringify({ deleted }));
}

function safeQuotaFileName(email) {
  const name = String(email || "").trim().toLowerCase().replace(/[<>:"/\\|?*\x00-\x1F]/g, "_");
  if (!name || /^\.+$/.test(name)) return "";
  return name + ".json";
}

function firstString(...values) {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}

function quotaFileData(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return {
    type: firstString(value.type, "codex") || "codex",
    email: emailFromRecord(value),
    expired: firstString(value.expired),
    id_token: firstString(value.id_token),
    account_id: firstString(value.account_id, value.chatgpt_account_id),
    disabled: value.disabled === true,
    access_token: firstString(value.access_token),
    last_refresh: firstString(value.last_refresh),
    refresh_token: firstString(value.refresh_token),
  };
}

function runGenerateQuotaFiles(db, req) {
  const ids = Array.isArray(req.ids) ? req.ids.map((id) => Number.parseInt(id, 10)).filter((id) => Number.isSafeInteger(id) && id > 0) : [];
  const outputDir = typeof req.output_dir === "string" ? req.output_dir.trim() : "";
  if (!outputDir) throw new Error("missing output_dir");
  fs.mkdirSync(outputDir, { recursive: true, mode: 0o700 });
  const select = db.prepare("SELECT id, record_json FROM data_records WHERE id = ?");
  const files = [];
  const seenFiles = new Set();
  let exported = 0;
  for (const id of ids) {
    const row = select.get(id);
    if (!row) continue;
    const data = quotaFileData(JSON.parse(row.record_json));
    if (!data) continue;
    const fileName = safeQuotaFileName(data.email);
    if (!fileName) continue;
    fs.writeFileSync(path.join(outputDir, fileName), JSON.stringify(data, null, 2), { encoding: "utf8", flag: "w", mode: 0o600 });
    exported += 1;
    if (!seenFiles.has(fileName)) {
      seenFiles.add(fileName);
      files.push(fileName);
    }
  }
  process.stdout.write(JSON.stringify({ exported, output_dir: outputDir, files }));
}

function runDeploy(db, req) {
  const ids = Array.isArray(req.ids) ? req.ids.map((id) => Number.parseInt(id, 10)).filter((id) => Number.isSafeInteger(id) && id > 0) : [];
  const outputDir = typeof req.output_dir === "string" ? req.output_dir.trim() : "";
  const target = typeof req.target === "string" ? req.target.trim() : "";
  if (!outputDir) throw new Error("missing output_dir");
  fs.mkdirSync(outputDir, { recursive: true, mode: 0o700 });
  const select = db.prepare("SELECT id, record_json FROM data_records WHERE id = ?");
  const update = db.prepare("UPDATE data_records SET record_json = ? WHERE id = ?");
  const files = [];
  const seenFiles = new Set();
  let deployed = 0;
  const now = new Date().toISOString();
  db.exec("BEGIN");
  try {
    for (const id of ids) {
      const row = select.get(id);
      if (!row) continue;
      const record = JSON.parse(row.record_json);
      const data = quotaFileData(record);
      if (!data) continue;
      const fileName = safeQuotaFileName(data.email);
      if (!fileName) continue;
      fs.writeFileSync(path.join(outputDir, fileName), JSON.stringify(data, null, 2), { encoding: "utf8", flag: "w", mode: 0o600 });
      deployed += 1;
      if (!seenFiles.has(fileName)) {
        seenFiles.add(fileName);
        files.push(fileName);
      }
      if (record && typeof record === "object" && !Array.isArray(record)) {
        record.lifecycle = "in_use";
        record.deployed_at = now;
        record.deploy_target = target || "local";
        update.run(JSON.stringify(record), id);
      }
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  process.stdout.write(JSON.stringify({ deployed, output_dir: outputDir, target: target || "local", files }));
}

function runRecycle(db, req) {
  const ids = Array.isArray(req.ids) ? req.ids.map((id) => Number.parseInt(id, 10)).filter((id) => Number.isSafeInteger(id) && id > 0) : [];
  const outputDir = typeof req.output_dir === "string" ? req.output_dir.trim() : "";
  if (!outputDir) throw new Error("missing output_dir");
  const select = db.prepare("SELECT id, record_json FROM data_records WHERE id = ?");
  const update = db.prepare("UPDATE data_records SET record_json = ? WHERE id = ?");
  const files = [];
  const seenFiles = new Set();
  let recycled = 0;
  db.exec("BEGIN");
  try {
    for (const id of ids) {
      const row = select.get(id);
      if (!row) continue;
      const record = JSON.parse(row.record_json);
      const data = quotaFileData(record);
      if (!data) continue;
      const fileName = safeQuotaFileName(data.email);
      if (!fileName) continue;
      const filePath = path.join(outputDir, fileName);
      if (fs.existsSync(filePath)) {
        fs.rmSync(filePath, { force: true });
        recycled += 1;
        if (!seenFiles.has(fileName)) {
          seenFiles.add(fileName);
          files.push(fileName);
        }
      }
      if (record && typeof record === "object" && !Array.isArray(record)) {
        record.lifecycle = "unused";
        delete record.deployed_at;
        delete record.deploy_target;
        update.run(JSON.stringify(record), id);
      }
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
  process.stdout.write(JSON.stringify({ recycled, output_dir: outputDir, files }));
}

function runExport(db, req) {
  const ids = Array.isArray(req.ids) ? req.ids.map((id) => Number.parseInt(id, 10)).filter((id) => Number.isSafeInteger(id) && id > 0) : [];
  const select = db.prepare("SELECT id, record_json FROM data_records WHERE id = ?");
  const lines = [];
  for (const id of ids) {
    const row = select.get(id);
    if (!row) continue;
    lines.push(JSON.stringify(JSON.parse(row.record_json)));
  }
  process.stdout.write(lines.join("\n") + "\n");
}

function main() {
  const req = JSON.parse(fs.readFileSync(0, "utf8"));
  if (!req.db_path) throw new Error("missing db_path");
  const db = connect(req.db_path);
  try {
    if (req.action === "import") {
      runImport(db, req);
      return;
    }
    if (req.action === "list") {
      runList(db, req);
      return;
    }
    if (req.action === "list_batches") {
      runListBatches(db);
      return;
    }
    if (req.action === "stats") {
      runStats(db);
      return;
    }
    if (req.action === "get_by_id") {
      runGetById(db, req);
      return;
    }
    if (req.action === "apply_refresh") {
      runApplyRefresh(db, req);
      return;
    }
    if (req.action === "update_state") {
      runUpdateState(db, req);
      return;
    }
    if (req.action === "update_batch") {
      runUpdateBatch(db, req);
      return;
    }
    if (req.action === "delete") {
      runDelete(db, req);
      return;
    }
    if (req.action === "generate_quota_files") {
      runGenerateQuotaFiles(db, req);
      return;
    }
    if (req.action === "deploy") {
      runDeploy(db, req);
      return;
    }
    if (req.action === "recycle") {
      runRecycle(db, req);
      return;
    }
    if (req.action === "update_quota") {
      runUpdateQuota(db, req);
      return;
    }
    if (req.action === "update_next_time") {
      runUpdateNextTime(db, req);
      return;
    }
    if (req.action === "register_reauth") {
      runRegisterReauth(db, req);
      return;
    }
    if (req.action === "export") {
      runExport(db, req);
      return;
    }
    throw new Error("unknown action");
  } finally {
    db.close();
  }
}

main();
`
