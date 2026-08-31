package datarecords

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	dataRecordsDBDirName  = "data"
	dataRecordsDBFileName = "data-records.sqlite"
)

// Store is the local data-records backend kept in custom-addon.
type Store struct {
	ConfigFilePath string
	AuthDir        string
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

// DataRecord is a JSONL row persisted for the management data page.
type DataRecord struct {
	ID         int64          `json:"id"`
	SourceFile string         `json:"source_file"`
	LineNumber int            `json:"line_number"`
	Summary    map[string]any `json:"summary,omitempty"`
	Data       any            `json:"data"`
	ImportedAt string         `json:"imported_at"`
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
}

type dataRecordsDeleteRequest struct {
	Action string  `json:"action"`
	DBPath string  `json:"db_path"`
	IDs    []int64 `json:"ids"`
	All    bool    `json:"all"`
}

type dataRecordsUpdateQuotaRequest struct {
	Action   string `json:"action"`
	DBPath   string `json:"db_path"`
	Email    string `json:"email"`
	NextTime string `json:"next_time"`
	Quota    string `json:"quota"`
	Status   string `json:"status"`
	Health   string `json:"health"`
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

type dataRecordsExportRequest struct {
	Action string  `json:"action"`
	DBPath string  `json:"db_path"`
	IDs    []int64 `json:"ids"`
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
}

type dataRecordsGenerateQuotaResponse struct {
	Exported  int      `json:"exported"`
	OutputDir string   `json:"output_dir"`
	Files     []string `json:"files"`
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

// GenerateQuotaFiles writes selected records as email-named JSON files under the local data directory.
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
	var payload dataRecordsGenerateQuotaResponse
	request := dataRecordsGenerateQuotaRequest{Action: "generate_quota_files", DBPath: dbPath, OutputDir: outputDir, IDs: ids}
	if errGenerate := runDataRecordsSQLite(request, &payload); errGenerate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to generate quota files: %v", errGenerate)})
		return
	}
	if payload.Files == nil {
		payload.Files = []string{}
	}
	c.JSON(http.StatusOK, payload)
}

func quotaFilesOutputDir(dbPath string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(dbPath)), ".cli-proxy-api")
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
	cmd := exec.Command(node, "--no-warnings", "-e", dataRecordsSQLiteScript)
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
      };
    }),
  };
  process.stdout.write(JSON.stringify(payload));
}

function runStats(db) {
  const rows = db.prepare("SELECT json_extract(record_json, '$.lifecycle') AS lifecycle, json_extract(record_json, '$.health') AS health, json_extract(record_json, '$.auth_state') AS auth_state, COUNT(*) AS n FROM data_records GROUP BY lifecycle, health, auth_state").all();
  const payload = { total: 0, lifecycle: {}, health: {}, auth_state: {} };
  for (const row of rows) {
    payload.total += row.n;
    bumpCount(payload.lifecycle, row.lifecycle, row.n);
    bumpCount(payload.health, row.health, row.n);
    bumpCount(payload.auth_state, row.auth_state, row.n);
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
  if (!email || (!nextTime && !quota && !status && !health)) {
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
    if (req.action === "update_quota") {
      runUpdateQuota(db, req);
      return;
    }
    if (req.action === "update_next_time") {
      runUpdateNextTime(db, req);
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
