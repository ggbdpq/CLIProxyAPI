package api

// 数据管理 SPA 静态资源服务：以 /data-mgmt/ 子路径 serve custom-addon/frontend/dist
// 构建产物，未命中文件时回退 index.html（SPA 客户端路由需要）。
// dist 定位与 data_management_extension.html 同款查找模式：
// 环境变量 CPA_DATA_MGMT_DIST 覆盖 → 配置目录 / 工作目录 / exe 目录向上逐级查找。

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

const (
	dataMgmtMountPath   = "/data-mgmt/"
	dataMgmtDistRelPath = "custom-addon/frontend/dist"
	dataMgmtDistEnvName = "CPA_DATA_MGMT_DIST"
)

// registerDataMgmtSPA 注册数据管理 SPA 的静态资源路由。
// 只注册 catch-all 一条：/data-mgmt（无尾斜杠）由 gin RedirectTrailingSlash 自动 301。
func (s *Server) registerDataMgmtSPA() {
	s.engine.GET(dataMgmtMountPath+"*path", s.serveDataMgmtSPA)
}

// serveDataMgmtSPA 输出 dist 内静态文件；无扩展名的路径视为前端路由，回退 index.html。
func (s *Server) serveDataMgmtSPA(c *gin.Context) {
	distRoot := s.resolveDataMgmtDist()
	if distRoot == "" {
		c.String(http.StatusServiceUnavailable,
			"数据管理前端未构建：请在 custom-addon/frontend 下执行 pnpm build，或通过 %s 指定 dist 目录", dataMgmtDistEnvName)
		return
	}

	rel := filepath.Clean("/" + c.Param("path")) // 以 / 开头的 Clean 结果不含 ..
	full := filepath.Join(distRoot, rel)
	if full != distRoot && !strings.HasPrefix(full, distRoot+string(filepath.Separator)) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	if info, err := os.Stat(full); err != nil || info.IsDir() || info.Mode().IsRegular() == false || filepath.Ext(full) == "" {
		// 目录、不存在或无扩展名（前端路由）：一律回退 index.html
		c.File(filepath.Join(distRoot, "index.html"))
		return
	}
	c.File(full)
}

// resolveDataMgmtDist 定位 dist 目录：环境变量优先，随后在配置目录/工作目录/exe 目录
// 所在目录链上逐级向上（最多 10 层）查找 custom-addon/frontend/dist。
func (s *Server) resolveDataMgmtDist() string {
	if override := strings.TrimSpace(os.Getenv(dataMgmtDistEnvName)); override != "" {
		if info, err := os.Stat(override); err == nil && info.IsDir() {
			return override
		}
		log.WithField("env", dataMgmtDistEnvName).Warn("override dist path is not a directory, fallback to lookup")
	}

	roots := make([]string, 0, 3)
	if s != nil && strings.TrimSpace(s.configFilePath) != "" {
		roots = append(roots, filepath.Dir(s.configFilePath))
	}
	if cwd, errCwd := os.Getwd(); errCwd == nil {
		roots = append(roots, cwd)
	}
	if exe, errExe := os.Executable(); errExe == nil {
		roots = append(roots, filepath.Dir(exe))
	}

	seen := make(map[string]struct{}, 8)
	for _, root := range roots {
		dir := root
		for range 10 {
			abs, errAbs := filepath.Abs(dir)
			if errAbs != nil {
				break
			}
			if _, dup := seen[abs]; dup {
				break
			}
			seen[abs] = struct{}{}
			candidate := filepath.Join(abs, dataMgmtDistRelPath)
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}
