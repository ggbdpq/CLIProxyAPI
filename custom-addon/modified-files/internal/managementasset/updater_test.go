package managementasset

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestFetchLatestAssetSetsGitHubAuthorization(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "asset-token")
	t.Setenv("GITSTORE_GIT_TOKEN", "")
	t.Setenv("GITSTORE_GIT_URL", "")

	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authorization = req.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assets":[{"name":"management.html","browser_download_url":"https://example.com/management.html","digest":"sha256:abc123"}]}`))
	}))
	defer server.Close()

	asset, remoteHash, err := fetchLatestAsset(t.Context(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchLatestAsset() error = %v", err)
	}
	if authorization != "Bearer asset-token" {
		t.Fatalf("Authorization = %q, want %q", authorization, "Bearer asset-token")
	}
	if asset == nil || asset.Name != managementAssetName {
		t.Fatalf("asset = %#v, want %q", asset, managementAssetName)
	}
	if remoteHash != "abc123" {
		t.Fatalf("remoteHash = %q, want %q", remoteHash, "abc123")
	}
}

func TestFetchLatestAssetOmitsAuthorizationWithoutToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("github_token", "")
	t.Setenv("GITSTORE_GIT_TOKEN", "")
	t.Setenv("GITSTORE_GIT_URL", "")

	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authorization = req.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assets":[{"name":"management.html","browser_download_url":"https://example.com/management.html","digest":"sha256:abc123"}]}`))
	}))
	defer server.Close()

	asset, remoteHash, err := fetchLatestAsset(t.Context(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchLatestAsset() error = %v", err)
	}
	if authorization != "" {
		t.Fatalf("Authorization = %q, want empty", authorization)
	}
	if asset == nil || asset.Name != managementAssetName {
		t.Fatalf("asset = %#v, want %q", asset, managementAssetName)
	}
	if remoteHash != "abc123" {
		t.Fatalf("remoteHash = %q, want %q", remoteHash, "abc123")
	}
}

func TestAutoUpdateSkipReason(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantReason string
		wantSkip   bool
	}{
		{
			name:       "nil config",
			cfg:        nil,
			wantReason: "config not yet available",
			wantSkip:   true,
		},
		{
			name: "cluster mode",
			cfg: &config.Config{
				Home: config.HomeConfig{Enabled: true},
			},
			wantReason: "cluster mode enabled",
			wantSkip:   true,
		},
		{
			name: "control panel disabled",
			cfg: &config.Config{
				RemoteManagement: config.RemoteManagement{DisableControlPanel: true},
			},
			wantReason: "control panel disabled",
			wantSkip:   true,
		},
		{
			name: "auto update disabled",
			cfg: &config.Config{
				RemoteManagement: config.RemoteManagement{DisableAutoUpdatePanel: true},
			},
			wantReason: "disable-auto-update-panel is enabled",
			wantSkip:   true,
		},
		{
			name:       "enabled",
			cfg:        &config.Config{},
			wantReason: "",
			wantSkip:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReason, gotSkip := autoUpdateSkipReason(tt.cfg)
			if gotReason != tt.wantReason || gotSkip != tt.wantSkip {
				t.Fatalf("autoUpdateSkipReason() = (%q, %t), want (%q, %t)", gotReason, gotSkip, tt.wantReason, tt.wantSkip)
			}
		})
	}
}

func TestInjectDataManagementExtension(t *testing.T) {
	input := []byte("<html><body><div id=\"root\"></div></body></html>")
	out := InjectDataManagementExtension(input)
	if !bytes.Contains(out, []byte("cpa-data-management-extension")) {
		t.Fatalf("extension marker missing from injected html")
	}
	if !bytes.Contains(out, []byte("/v0/management/data-records/import")) {
		t.Fatalf("data import endpoint missing from injected html")
	}
	for _, want := range [][]byte{
		[]byte("dataColumns(records)"),
		[]byte("formatCellValue"),
		[]byte("<th>ID</th>"),
		[]byte("cpa-data-select-all-label"),
		[]byte("\u9009\u62e9</label>"),
		[]byte("<th>\u64cd\u4f5c</th>"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("dynamic data columns missing %q", want)
		}
	}
	if bytes.Contains(out, []byte(`title="复制文案"`)) || bytes.Contains(out, []byte("copyCellText")) {
		t.Fatal("data cells should not render per-column copy buttons")
	}
	if !bytes.Contains(out, []byte("-webkit-line-clamp: 2")) {
		t.Fatal("two-line data cell clamp missing")
	}
	for _, removed := range [][]byte{
		[]byte("source_file"),
		[]byte("imported_at"),
	} {
		if bytes.Contains(out, removed) {
			t.Fatalf("removed metadata column still present %q", removed)
		}
	}

	again := InjectDataManagementExtension(out)
	if bytes.Count(again, []byte("cpa-data-management-extension")) != 1 {
		t.Fatalf("extension should be injected once")
	}
}

func TestInjectDataManagementExtensionLeavesHTMLWithoutBodyCloseUnchanged(t *testing.T) {
	input := []byte("<html><body></body-close-missing></html>")
	out := InjectDataManagementExtension(input)
	if string(out) != string(input) {
		t.Fatalf("html without body close should be unchanged")
	}
}

func TestDataManagementExtensionPrioritizesAndFormatsQuotaAndNextTime(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("prioritizeNextTimeColumn"),
		[]byte("formatQuotaValue"),
		[]byte("formatNextTimeValue"),
		[]byte("hasQuota ? '<th>\u989d\u5ea6</th>'"),
		[]byte("cellHTML(data.quota, 'quota')"),
		[]byte("cellHTML(data.nextTime, 'nextTime')"),
		[]byte("return text + ' 00:00'"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing quota/nextTime ordering or format support %q", want)
		}
	}
}

func TestDataManagementExtensionPrioritizesStatusBeforeNextTime(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("hasStatus ? '<th>status</th>'"),
		[]byte("cellHTML(data.status, 'status')"),
		[]byte("if (columns.indexOf('status') !== -1) priority.push('status')"),
		[]byte("key !== 'quota' && key !== 'status' && key !== 'nextTime'"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing status ordering support %q", want)
		}
	}
}

func TestDataManagementExtensionProvidesManagementKeyInput(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("cpa-data-management-key"),
		[]byte("\u8bf7\u8f93\u5165\u7ba1\u7406\u5bc6\u94a5"),
		[]byte("\u7f3a\u5c11\u7ba1\u7406\u5bc6\u94a5"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing %q", want)
		}
	}
}

func TestDataManagementExtensionSupportsDataSearch(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("cpa-data-search"),
		[]byte("cpa-data-search-btn"),
		[]byte("\u641c\u7d22"),
		[]byte("\u641c\u7d22 email\u3001status \u6216 nextTime"),
		[]byte("encodeURIComponent(searchQuery)"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing data search support %q", want)
		}
	}
}

func TestDataManagementExtensionMarksNavItemActive(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("nav.classList.add('active')"),
		[]byte("nav.classList.remove('active')"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing %q", want)
		}
	}
}

func TestDataManagementExtensionSupportsSelectedRowDeletion(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("selectedRecordIDs"),
		[]byte("<button id=\"cpa-data-delete-selected\""),
		[]byte("\u5220\u9664\u9009\u4e2d"),
		[]byte("<button id=\"cpa-data-delete-all\""),
		[]byte("\u5168\u90e8\u5220\u9664"),
		[]byte("deleteAllRecords"),
		[]byte("{ all: true }"),
		[]byte("cpa-data-row-select"),
		[]byte("/v0/management/data-records"),
		[]byte("method: 'DELETE'"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing deletion support %q", want)
		}
	}
}

func TestDataManagementExtensionSupportsSelectAllRows(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("cpa-data-select-all"),
		[]byte("toggleAllDataRecords"),
		[]byte("syncSelectAllCheckbox"),
		[]byte("checkbox.indeterminate"),
		[]byte("selectableDataRecordIDs"),
		[]byte("pruneSelectedRecordIDs"),
		[]byte("\u5168\u9009\u5f53\u524d\u5217\u8868"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing select-all support %q", want)
		}
	}
}

func TestDataManagementExtensionSupportsQuotaGeneration(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("<button id=\"cpa-data-generate-quota\""),
		[]byte("\u751f\u6210\u914d\u989d"),
		[]byte("generateQuotaFiles"),
		[]byte("showQuotaResultDialog"),
		[]byte("\u67e5\u770b\u8be6\u60c5"),
		[]byte("cpa-quota-result"),
		[]byte("/v0/management/data-records/generate-quota"),
		[]byte("method: 'POST'"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing quota generation support %q", want)
		}
	}
}

func TestDataManagementExtensionSupportsPagination(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("cpa-data-pagination"),
		[]byte("cpa-data-page-btn"),
		[]byte("cpa-data-page-info"),
		[]byte("goToPage"),
		[]byte("pageSize"),
		[]byte("currentPage"),
		[]byte("?limit="),
		[]byte("&offset="),
		[]byte("\u4e0a\u4e00\u9875"),
		[]byte("\u4e0b\u4e00\u9875"),
		[]byte("currentPage > 1"),
		[]byte("pruneSelectedRecordIDs"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing pagination support %q", want)
		}
	}
}

func TestDataManagementExtensionSupportsExportJSONL(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("\u5bfc\u51fa JSONL"),
		[]byte("exportJSONL"),
		[]byte("cpa-data-export"),
		[]byte("/v0/management/data-records/export"),
		[]byte("createObjectURL"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing export jsonl support %q", want)
		}
	}
}

func TestDataManagementExtensionSupportsQuotaCardDelete(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("cpa-quota-card-delete"),
		[]byte("enhanceQuotaCards"),
		[]byte("/v0/management/auth-files"),
		[]byte("QuotaCard-module__fileName"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing quota card delete support %q", want)
		}
	}
}

func TestUsePluginInjectedUIReadsEnv(t *testing.T) {
	t.Setenv(pluginUIEnvName, "")
	if UsePluginInjectedUI() {
		t.Fatal("empty CPA_PLUGIN_UI should keep Go injection")
	}
	t.Setenv(pluginUIEnvName, "1")
	if !UsePluginInjectedUI() {
		t.Fatal("CPA_PLUGIN_UI=1 should skip Go injection")
	}
}

func TestInjectDataManagementExtensionLoadsFromCustomAddon(t *testing.T) {
	path := dataManagementExtensionPath()
	if path == "" {
		t.Fatal("expected custom-addon/frontend/data_management_extension.html")
	}
	if !strings.Contains(filepath.ToSlash(path), "custom-addon/frontend/data_management_extension.html") {
		t.Fatalf("path = %q, want custom-addon frontend html", path)
	}
}

func TestDataManagementExtensionInventoryAndBatchLedgerMarkers(t *testing.T) {
	out := InjectDataManagementExtension([]byte(`<html><body><div id="root"></div></body></html>`))
	for _, want := range [][]byte{
		[]byte("cpa-data-stat-total"),
		[]byte("cpa-data-stat-unused"),
		[]byte("cpa-data-stat-inuse"),
		[]byte("cpa-data-stat-sold"),
		[]byte("cpa-data-stat-abnormal"),
		[]byte("abnormalHealthCount"),
		[]byte("cpa-data-filter-lifecycle"),
		[]byte("cpa-data-filter-health"),
		[]byte("cpa-data-filter-auth"),
		[]byte("cpa-data-filter-batch"),
		[]byte("cpa-data-filter-clear"),
		[]byte("cpa-data-apply-lifecycle"),
		[]byte("/v0/management/data-records/update-state"),
		[]byte("/v0/management/data-records/stats"),
		[]byte("cpa-data-detect-selected"),
		[]byte("cpa-data-detect-filtered"),
		[]byte("cpa-data-detect-stop"),
		[]byte("wham/usage"),
		[]byte("X-Cpa-Data-Email"),
		[]byte("cpa-data-batch-nav"),
		[]byte("cpa-data-batch-page"),
		[]byte("batch-ledger"),
		[]byte("/v0/management/data-records/batches"),
		[]byte("/v0/management/data-records/update-batch"),
		[]byte("renderBatchPage"),
		[]byte("saveBatchMeta"),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("injected html missing inventory/batch marker %q", want)
		}
	}
}
