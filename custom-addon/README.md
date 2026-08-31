# 数据管理定制模块（custom-addon）

在官方 `CLIProxyAPI` 之上追加「数据管理」功能的自包含模块：后端是 module 内独立 Go 包（直接编译进二进制），前端是独立 React SPA（`/data-mgmt/` 子路径访问，与官方面板零耦合）。对官方源码的侵入压缩到 **2 个被修改的官方文件 + 2 个新增文件**，升级走 rebase 一条线（见 `docs/升级与维护.md`）。

```
custom-addon/
├── README.md                          # 本文件
│
├── docs/                              # 文档集中归档
│   ├── 使用指南.md                     # 数据管理版使用说明
│   ├── 升级与维护.md                   # 官方同步策略与补丁重放指南（重点阅读）
│   └── CPA对比分析报告.md              # 历史 fork 对比结论（时点性文档）
│
├── scripts/                           # 脚本集中归档（ps1 均为 UTF-8 BOM）
│   ├── build.ps1                      # 前端 pnpm build + Go 编译串联（-SkipFrontend 可跳前端）
│   ├── backup-data.ps1                # 备份 data-records.sqlite 与 config.yaml
│   └── verify-archive.ps1             # 校验 modified-files/ 与源码树一致
│
├── backend/                           # 后端核心（单一来源，module 内 import 编译进二进制）
│   ├── data_records.go                #   数据管理 handler + 内嵌 node:sqlite 脚本（package datarecords）
│   └── data_records_test.go           #   对应测试
│
├── frontend/                          # 前端 SPA（单一来源，构建产出 dist/ 不入库）
│   ├── src/routes/                    #   index.tsx（数据记录）/ batches.tsx（批次台账）/ login.tsx（密钥登录）
│   ├── src/lib/                       #   api.ts（Bearer 注入+401 处理）/ queries.ts / schemas.ts（Zod）
│   ├── src/store/app.ts               #   Zustand：筛选/选中/全局检测循环
│   └── src/components/                #   records 组件 + shadcn/ui
│
├── modified-files/                    # 修改过的官方文件快照（独立 module 不参与编译，verify-archive.ps1 校验）
│   ├── go.mod                         #   刻意的独立 module，防 go build ./... 误编译归档副本
│   └── internal/api/
│       ├── server_management.go                        # 9 条数据路由 + dataRecordsStore() + SPA 挂载
│       └── handlers/management/api_tools.go            # 数据记录处理 + /api-call 代理 + 配额回写
│
└── exe/                               # 编译产物（.gitignore 忽略，换机需重新编译）
    └── cli-proxy-api-datamgmt.exe     #   唯一 exe（旧 8317/8318 双形态已退役）
```

> 另有源码树新增文件（不在本目录）：`internal/api/data_mgmt_spa.go`（`/data-mgmt/` 静态 serve + SPA fallback）与 `internal/api/handlers/management/api_tools_quota_test.go`（配额回写测试）。

---

## 一、功能是什么

独立 SPA「数据管理」面板（`http://127.0.0.1:<端口>/data-mgmt/`）：**JSONL 导入（去重）/ 导出 / 列表（动态列）/ 搜索 / 筛选（生命周期/健康/授权/批次）/ 分页 / 删除（单条/选中/全部）/ 批量改状态 / 批量生成配额文件 / 配额检测（自动循环 + 429 退避）/ 批次台账（行内编辑订单链接与备注）**。数据写入本地 SQLite（`data/data-records.sqlite`）；Codex 账号配额检测经后端 `/v0/management/api-call` 代理转发 `chatgpt.com`，前端只同源通信。

## 二、后端 API 契约

| 方法与路径 | 请求 | 响应关键字段 |
|---|---|---|
| `GET /v0/management/data-records` | query：`limit/offset/q/lifecycle/health/auth_state/batch` | `total`, `records[]` |
| `GET /v0/management/data-records/stats` | — | `total`, `lifecycle/health/auth_state` 计数 map |
| `GET /v0/management/data-records/batches` | — | `total`, `batches[]`（含健康/授权分布、订单元数据） |
| `POST /v0/management/data-records/import?dedupe=1` | multipart `file` | `imported`, `stats` |
| `GET /v0/management/data-records/export?ids=1,2` | query：ID 列表 | 下载 JSONL |
| `DELETE /v0/management/data-records` | JSON `{ids}` 或 `{all:true}` | `deleted` |
| `POST /v0/management/data-records/update-state` | JSON `{ids, lifecycle}` | `updated` |
| `POST /v0/management/data-records/update-batch` | JSON `{batch_key, order_url, notes}` | — |
| `POST /v0/management/data-records/generate-quota` | JSON `{ids}` | `exported`, `output_dir`, `files[]` |
| `POST /v0/management/api-call` | JSON `{url, method, headers, body}` | 上游响应透传（配额检测用） |

认证：`Authorization: Bearer <管理密钥>`（与整个管理 API 共用；SPA 登录页输入后存 localStorage `cpa-data-management-key`）。存在按 IP 的失败尝试限流（错误密钥连打会触发约 25 分钟封禁，重启清零）。

## 三、本地启动

均在**仓库根**（`config.yaml` 所在目录）执行，端口以 `config.yaml` 为准：

```powershell
# 方式①：源码直跑（改 Go 源码后重跑即可）
go run ./cmd/server --config config.yaml

# 方式②：编译 exe（前端 + 后端串联构建）
powershell -ExecutionPolicy Bypass -File custom-addon\scripts\build.ps1
custom-addon\exe\cli-proxy-api-datamgmt.exe --config config.yaml
```

- 数据管理面板：`http://127.0.0.1:<端口>/data-mgmt/`；官方面板：`/management.html`（纯官方原样，无注入）。
- **不要直接双击 exe**：必须在仓库根带 `--config config.yaml` 启动。
- 改前端源码：`cd custom-addon\frontend; pnpm build` 后重启服务（或开发模式 `pnpm dev` 热更，`/v0` 自动代理到 127.0.0.1:8317）。
- 运行时依赖：后端 SQLite 操作委托给带内建 `node:sqlite` 的 Node（v22.5+/v24+）。查找顺序：`CLIPROXY_SQLITE_NODE` 环境变量 → PATH 中的 `node` → `~/.cache/codex-runtimes/.../node.exe`。

## 四、安全注意事项

- 管理密钥错误连打触发 IP 限流封禁（内存态，重启清零）。
- 导入的数据（如 OA 授权记录）含敏感凭证（email / password / access_token），`data/` 不入版本控制但明文落盘，注意保管。
- 「生成配额」会把选中记录写成 `.cli-proxy-api/<email>.json` 的 Codex 凭证文件（含 access_token / refresh_token），即代理可直接使用的账号文件，务必确认选中范围。
- 旧的注入形态（HTML 拼接 + Chrome 扩展 + `CPA_PLUGIN_UI` 环境变量）已于 2026-09 退役，回滚点为 tag `legacy-inject-final`。
