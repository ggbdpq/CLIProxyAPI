# 数据管理功能 —— 独立参考模块

将「数据管理」功能整理成一个自包含的模块目录：后端是 module 内的独立 Go 包（直接编译进二进制），前端是运行时加载的 HTML 资源，Chrome 插件提供免注入的替代形态。对官方源码的侵入被压缩到 **4 个被修改的文件 + 1 个新增测试文件**（见 `modified-files/`），升级官方版本时只需重放这一小片补丁面。

```
custom-addon/
├── README.md                          # 本文件
│
├── docs/                              # 文档集中归档
│   ├── 使用指南.md                     # 数据管理版使用说明（含打包 exe 方法）
│   ├── 升级与维护.md                   # 官方同步策略与补丁重放指南（重点阅读）
│   └── CPA对比分析报告.md              # 历史 fork 对比结论（时点性文档，结论以本 README 为准）
│
├── scripts/                           # 脚本集中归档
│   ├── start-8317.bat                 # 8317 启动器（GBK+CRLF，勿用 UTF-8 编辑器保存；自动设 CPA_PLUGIN_UI=1）
│   ├── build.ps1                      # 编译一份二进制并复制为两种 exe（UTF-8 BOM）
│   ├── backup-data.ps1                # 备份 data-records.sqlite 与 config.yaml
│   └── verify-archive.ps1             # 校验 modified-files/ 与源码树一致
│
├── backend/                           # 新增文件：后端核心（单一来源，module 内 import 编译进二进制）
│   ├── data_records.go                #   数据管理 handler + 内嵌 node:sqlite 脚本（package datarecords）
│   └── data_records_test.go           #   对应测试
│
├── frontend/                          # 新增文件：前端扩展资源（单一来源，运行时按路径加载）
│   └── data_management_extension.html #   管理面板「数据管理」页
│
├── modified-files/                    # 修改文件：完整拷贝，保留原相对路径（与源码树同步，verify-archive.ps1 校验）
│   ├── config.yaml                    #   本机运行配置样例（端口 8318；config.example.yaml 已不打补丁）
│   └── internal/
│       ├── api/server_management.go                        # 5 条数据路由 + 注入分支
│       ├── api/handlers/management/api_tools.go            # Codex 配额回写
│       ├── api/handlers/management/api_tools_quota_test.go # 配额回写测试（新增文件）
│       └── managementasset/
│           ├── updater.go / updater_test.go                # InjectDataManagementExtension + CPA_PLUGIN_UI 开关
│
├── exe/                               # 编译产物（.gitignore 忽略，换机需重新编译）
│   ├── cli-proxy-api-8317.exe         #   插件注入形态（启动时设 CPA_PLUGIN_UI=1，与 datamgmt 是同一份二进制）
│   └── cli-proxy-api-datamgmt.exe     #   Go 注入形态（默认）
│
└── chrome-extension/                  # Chrome 插件：前端注入的浏览器端实现
    ├── manifest.json                  #   MV3，content_scripts world=MAIN
    ├── content.js                     #   页面逻辑（提取自 data_management_extension.html）
    ├── content.css                    #   样式（同上）
    └── README.md                      #   安装与使用说明
```

> 注：`docs/CPA对比分析报告.md` 内的文件链接是当时 VSCode 里的 webview 链接，已失效；对应关系以本 README 的文件清单为准。

---

## 一、功能是什么

管理面板新增「数据管理」页：**JSONL 导入 / 导出 / 列表 / 搜索 / 分页 / 删除 / 批量生成配额**。数据写入本地 SQLite（`data/data-records.sqlite`，表 `data_records`）；Codex 账号请求 `/wham/usage` 后自动回写配额重置时间与剩余额度。

## 二、后端 API 契约（前端已验证）

| 方法与路径 | 请求 | 响应关键字段 |
|---|---|---|
| `GET /v0/management/data-records?limit=&offset=&q=` | query：分页 + 搜索 | `total`, `records[]`（含 `id`, `data`, `summary`） |
| `POST /v0/management/data-records/import` | multipart `file`（或 body + `?filename=`） | `imported`, `status` |
| `GET /v0/management/data-records/export?ids=1,2,3` | query：ID 列表 | 下载 JSONL |
| `DELETE /v0/management/data-records` | JSON `{ "ids": [...] }`（或 `{ "all": true }`） | `deleted` |
| `POST /v0/management/data-records/generate-quota` | JSON `{ "ids": [...] }` | `exported`, `output_dir`, `files[]` |

认证：`Authorization: Bearer <管理密钥>`（与整个管理 API 共用）。注意存在按 IP 的失败尝试限流（错误密钥连打会触发约 25 分钟封禁，重启服务清零）。

## 三、前端注入的两种方式（选一）

| | Go 注入（默认形态） | Chrome 插件注入 |
|---|---|---|
| 启用方式 | 默认。返回官方面板前把扩展 HTML 拼到 `</body>` 前 | 启动服务时设 `CPA_PLUGIN_UI=1`，直接返回官方原始面板 |
| Go 源码 | `updater.go`（注入函数 + 扩展路径查找）+ `server_management.go`（注入分支） | **不需要**改前端注入逻辑 |
| 官方升级抗覆盖 | 会被升级冲掉，需重新打补丁 | 前端注入随浏览器插件走，升级不覆盖 |
| 前端代码位置 | `frontend/data_management_extension.html` | `chrome-extension/content.js` + `content.css` |
| 后端 API 依赖 | 必须 | **同样必须**（插件只替代前端注入层） |
| 运行环境 | 无额外依赖 | Chrome 111+ |

**无论哪种方式，`backend/data_records.go` 与 `modified-files/...` 中的后端改动都必须存在于 Go 源码**，插件无法提供 HTTP 端点。

两种形态共用**同一份二进制**，靠环境变量 `CPA_PLUGIN_UI` 切换（实现在 `internal/managementasset/updater.go` 的 `UsePluginInjectedUI()`，设为 `1` 或 `true` 生效）；`CPA_DATA_MGMT_HTML` 可覆盖扩展 HTML 的加载路径。二者共享幂等标记（`cpa-data-management-extension` / `window.__cpaDataMgmtInjected`），不会双重渲染。

## 四、移植到其它版本（补丁思路）

1. **新增文件**：把 `backend/` 拷入模块（package `datarecords`，module 内 import 即可）；把 `frontend/data_management_extension.html` 放到可被定位的 `custom-addon/frontend/` 路径（从 config 目录 / cwd / exe 目录向上查找）。
2. **修改文件**：对照 `modified-files/` 里的完整文件，应用四处增量改动——
   - `server_management.go`：注册 5 条路由 + `dataRecordsStore()` + 注入分支。
   - `api_tools.go`：`syncCodexQuotaNextTimeFromAPICall()` 配额回写 + `dataRecordsStore()`。
   - `updater.go`：`UsePluginInjectedUI()` + `InjectDataManagementExtension()` + 扩展路径查找。
   - `updater_test.go`：注入 / 开关 / UI 功能测试；另加新增文件 `api_tools_quota_test.go`（配额回写测试）。
3. **配置**：`config.example.yaml` 保持官方原样；本地差异（端口 8318、`auth-dir` 等）只放在 `config.yaml`（不入库）。
4. **前端注入**：按上方第三节二选一。

## 五、本地启动（三种方式）

均在**仓库根**（`config.yaml` 所在目录）执行：

| 方式 | 命令 | 端口 / 管理面板 | 前端注入 |
|---|---|---|---|
| ① 源码直跑（推荐，始终与源码一致） | `go run ./cmd/server --config config.yaml` | 8318 / http://localhost:8318/management.html | 服务端注入，无需插件 |
| ② 编译 exe 跑 | 先 `powershell -ExecutionPolicy Bypass -File custom-addon\scripts\build.ps1`，再 `custom-addon\exe\cli-proxy-api-datamgmt.exe --config config.yaml` | 8318 / 同上 | 服务端注入，无需插件 |
| ③ 插件注入形态 | `custom-addon\scripts\start-8317.bat`（自动设 `CPA_PLUGIN_UI=1`） | 8317 / http://localhost:8317/management.html | Chrome 插件注入，需先在 `chrome://extensions/` 加载 `custom-addon/chrome-extension/` |

- **不要直接双击 exe**：必须在仓库根带 `--config config.yaml` 启动，否则以 exe 所在目录为工作目录，报找不到配置文件。
- 改了 Go 源码后：方式① 重跑即可；方式②/③ 需先跑 `build.ps1` 重编（服务运行中时 exe 被占用，先停服务）。
- 验证：`curl http://127.0.0.1:8318/healthz`；客户端接入 `Base URL: http://127.0.0.1:8318/v1`，API Key 取 `config.yaml` 的 `api-keys`；面板登录密钥为 `remote-management.secret-key` 哈希对应的明文（`start-8317.bat` 启动时会显示）。
- 更多细节（OAuth 登录、TUI、常见问题）见 `docs/使用指南.md`。

### 运行现状

- **8317（插件注入形态）**：在 `C:\Users\802165\CLIProxyAPI` 目录运行（读那边官方 `config.yaml`，`auth-dir` 复用官方 `.cli-proxy-api` token）。
- **8318（Go 注入形态）**：在本仓库目录运行（本仓库 `config.yaml`，端口 8318、`auth-dir: ./.cli-proxy-api`，已有 52 个 Codex 凭证，无需重新登录）。
- 数据库：8317 空库在 `C:\Users\802165\CLIProxyAPI\data\data-records.sqlite`；8318 库在本仓库 `data/data-records.sqlite`。
- 运行时依赖：后端 SQLite 操作委托给带内建 `node:sqlite` 的 Node（v22.5+/v24+）。节点查找顺序：`CLIPROXY_SQLITE_NODE` 环境变量 → PATH 中的 `node` → `~/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe`。

## 六、安全注意事项

- 数据管理 API 与整个管理 API 共用密钥；**错误密钥连打会触发 IP 限流封禁**（内存态，重启服务清零）。
- 导入的数据（如 OA 授权记录）含敏感凭证（email / password / access_token），`data/` 目录未纳入版本控制，但文件明文落盘，注意保管。
- 「生成配额」会把选中记录写成 `.cli-proxy-api/<email>.json` 的 Codex 凭证文件（含 access_token / refresh_token），即代理可直接使用的账号文件，务必确认选中范围。
- 插件仅匹配 `localhost:8317`，不采集、不外发数据；密钥仅经页面自身存储处理。
