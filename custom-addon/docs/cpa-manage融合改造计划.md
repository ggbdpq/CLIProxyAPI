# cpa-manage 融合改造计划

> 日期：2026-09-01
> 目标读者：接手 `CLIProxyAPI/custom-addon` 的开发者。
> 覆盖范围：把旧项目 `C:\Users\802165\vibe\guahub\cpa-manage` 中仍有价值的账号库存能力，按新项目 `E:\ggbdpq\guahub\gh\fork\CLIProxyAPI` 的现行架构融合进来。本文先定边界，再作为后续开发验收清单。

## 一、核心结论

旧项目不能整包搬进来，只把缺口能力迁移到新架构里。

| 优先级 | 结论 | 本轮动作 |
|---|---|---|
| P0 | 保留现行 `custom-addon/backend` + React SPA + `/data-mgmt/`，补齐部署/回收闭环 | 已完成 `deploy` / `recycle` API、前端按钮和单测 |
| P1 | 迁移自动检测的结果过滤、Token 刷新、凭证字段回写 | 已完成后端筛选/统计、Token 刷新 API 与前端按钮 |
| P2 | 格式转换、重授权登记、工具页 | 已完成工具页、转换纯函数、重授权登记和单测 |
| 不迁移 | HTML 注入、Chrome 扩展、旧 exe、旧运行数据目录 | 这些属于旧承载方式或本机运行产物，不进入新项目源码 |

## 二、当前事实地图

以下事实来自本轮只读盘点和基线命令，后续开发按这些文件落点推进。

| 领域 | 新项目现状 | 旧项目可参考点 | 判断 |
|---|---|---|---|
| 后端入口 | `E:\ggbdpq\guahub\gh\fork\CLIProxyAPI\internal\api\server_management.go` 已注册 14 条 `/v0/management/data-records*` 路由 | 旧项目同文件多出部署/回收/刷新能力 | 已补齐部署、回收、刷新、转换、重授权登记 |
| 数据处理 | `custom-addon/backend/data_records.go` 通过内嵌 `node:sqlite` 脚本读写 `data/data-records.sqlite` | 旧项目已在同一模型里实现 deploy/recycle | 复用现有脚本模型，不引入 Go SQLite 依赖 |
| 前端形态 | `custom-addon/frontend` 是 React 19 + Vite SPA，挂载 `/data-mgmt/` | 旧项目 `data_management_extension.html` 是注入式单文件 UI | 只迁移行为，不迁移注入 UI |
| 官方面板 | `custom-addon/frontend/src/routes/panel.tsx` 用 iframe 反向嵌入 `/management.html#/system` | 旧项目改 `updater.go` 注入官方 DOM | 新项目保持零侵入官方面板 |
| 归档校验 | `custom-addon/scripts/verify-archive.ps1` 校验官方补丁面和单一来源文件 | 旧项目归档包含 `updater.go` 注入文件 | 新项目不恢复 `updater.go` 注入归档 |
| 基线 | `go test ./custom-addon/backend ./internal/api/handlers/management ./internal/api` 已通过 | 旧项目有 deploy/recycle 测试样例 | 最终仍按验证清单重跑 |

## 三、能力缺口与迁移方式

| 能力 | 业务价值 | 现有复用点 | 实现代价 | 失败模式 | 优先级 |
|---|---|---|---|---|---|
| 部署账号到 auth 目录 | 选中库存后直接生成可被代理读取的 `<email>.json` | 现有 `GenerateQuotaFiles`、`quotaFileData`、`safeQuotaFileName` | 小，新增白名单目标和状态回写 | 任意路径写入、覆盖错误目录、状态未回写 | P0 |
| 回收账号文件 | 从代理 auth 目录移除失效账号并回到库存 | 旧项目 `runRecycle`、现有 ID 查询 | 小，删除白名单目录下的 email 文件 | 删除范围失控、文件删了但库存没回退 | P0 |
| 部署状态字段 | 记录 `lifecycle=in_use`、`deployed_at`、`deploy_target` | JSON 动态列天然展示 | 小，无需改表结构 | 字段名分裂或旧按钮文案误导 | P0 |
| 已检测过滤/统计 | 快速找到已回写 quota/nextTime 的账号 | 旧项目 `detected` 统计与筛选 | 中，需要前后端筛选一致 | 统计含义和健康状态混淆 | P1 |
| Token 刷新 | 用 refresh_token 主动换新 access_token | 旧项目 `RefreshDataRecordToken` | 中，涉及网络、代理、错误状态 | 刷新接口变化、误判 needs_reauth、泄露 token | P1 |
| 格式转换工具页 | 把外部卡密格式转成库存 JSONL / sub2api | 已观察到 JSON 对象行、CPA JSONL、sub2api bundle | 中，先支持三种明确方向 | 非 JSON 卡密行需后续样本扩展 | P2 |
| 重授权登记 | OA 重授权产物按邮箱回写库存 | 现有 JSONL 导入和邮箱匹配 | 中，复用 SQLite 脚本 | 字段缺失或邮箱未匹配 | P2 |

## 四、P0 设计

### 4.1 后端接口

| 接口 | 入参 | 行为 | 响应 |
|---|---|---|---|
| `POST /v0/management/data-records/deploy` | `{ "ids": number[], "target": "local" | "official" }` | 只写白名单目录，按 email 生成 `<email>.json`，回写 `lifecycle=in_use`、`deployed_at`、`deploy_target` | `{ "deployed": number, "output_dir": string, "files": string[] }` |
| `POST /v0/management/data-records/recycle` | `{ "ids": number[], "target": "local" | "official" }` | 只删除白名单目录下由 email 推导出的 JSON 文件，回写 `lifecycle=unused`，删除部署字段 | `{ "recycled": number, "output_dir": string, "files": string[] }` |

白名单目标先保留两个：

| key | 目录 | 用途 |
|---|---|---|
| `local` | 从 `data-records.sqlite` 上推到当前项目 `.cli-proxy-api` | 当前管理舱本地验证 |
| `official` | `C:\Users\802165\CLIProxyAPI\.cli-proxy-api`，可用 `CPA_OFFICIAL_AUTH_DIR` 覆盖 | 统一端口代理读取目录 |

不支持任意路径。后续真要多目录，增加 key，不加自由输入框。

### 4.2 前端入口

库存页只加一组小控件：

| 控件 | 行为 |
|---|---|
| 目标下拉 | `local` / `official` |
| `部署选中` | 调 `/deploy`，成功后弹出目标目录和文件名 |
| `回收选中` | 二次确认后调 `/recycle`，成功后弹出目标目录和文件名 |
| 旧 `生成配额文件` | 保留兼容端点，前端文案收敛为「兼容生成」 |

### 4.3 验收标准

1. 部署选中账号后，目标目录出现 `<email>.json`，文件内容仍是现有凭证字段集合。
2. 部署后列表中对应记录出现 `lifecycle=in_use`、`deployed_at`、`deploy_target`。
3. 回收后目标目录中对应文件消失，记录回到 `lifecycle=unused`，部署字段被清掉。
4. `target` 非白名单时返回 400。
5. 重复 ID、重复 email 不产生重复文件名响应。

## 五、不迁移清单

| 旧资产 | 处理 |
|---|---|
| `custom-addon/frontend/data_management_extension.html` | 不迁移，React SPA 已替代 |
| `custom-addon/chrome-extension/*` | 不迁移，扩展形态冻结 |
| `internal/managementasset/updater.go` 注入逻辑 | 不恢复，继续零侵入官方 `management.html` |
| `custom-addon/exe/*`、根目录 `cli-proxy-api.exe` | 不迁移，属于构建产物 |
| `data/`、`logs/`、`backup/`、`.cli-proxy-api/` | 不迁移，属于运行数据或备份 |
| 旧项目 `.gitignore` 的无关差异 | 只吸收运行产物忽略思路，不整文件覆盖 |

## 六、验证与回滚

改动后按从小到大验证：

| 层级 | 命令 | 预期 |
|---|---|---|
| Go 单测 | `$env:GOCACHE='E:\ggbdpq\.cache\go-build'; go test ./custom-addon/backend ./internal/api/handlers/management ./internal/api` | 三个包通过 |
| Go 编译 | `$env:GOCACHE='E:\ggbdpq\.cache\go-build'; go build -o E:\ggbdpq\.cache\cliproxyapi-build\cli-proxy-api.exe ./cmd/server` | 生成临时 exe |
| 前端类型 | `pnpm.cmd --dir E:\ggbdpq\guahub\gh\fork\CLIProxyAPI\custom-addon\frontend typecheck` | TypeScript 通过 |
| 前端构建 | `pnpm.cmd --dir E:\ggbdpq\guahub\gh\fork\CLIProxyAPI\custom-addon\frontend build` | dist 构建通过 |
| 归档 | `powershell -ExecutionPolicy Bypass -File E:\ggbdpq\guahub\gh\fork\CLIProxyAPI\custom-addon\scripts\verify-archive.ps1` | 官方补丁归档一致，单一来源存在 |
| Diff | `git -C E:\ggbdpq\guahub\gh\fork\CLIProxyAPI diff --check` | 无空白错误 |

回滚方式：本轮只改源码和文档，不主动操作真实库存库，也不删除真实 auth 文件。若需回退，反向还原本轮改动文件即可；如果人工调用过回收接口，需要从对应 auth 目录备份恢复 `<email>.json`。

## 七、下一步执行

P0/P1/P2 均已按新架构完成；后续只在出现真实新格式样本时扩展转换规则。

## 八、2026-09-01 P0 实施记录

已按本文 P0 先完成最小垂直切片：

| 文件 | 改动 |
|---|---|
| `custom-addon/backend/data_records.go` | 新增目标目录白名单、`DeployDataRecords`、`RecycleDataRecords`、内嵌 SQLite 脚本的 `deploy` / `recycle` action |
| `custom-addon/backend/data_records_test.go` | 新增部署、回收、非法 target 三组单测 |
| `internal/api/server_management.go` | 注册 `/v0/management/data-records/deploy` 与 `/v0/management/data-records/recycle` |
| `custom-addon/modified-files/internal/api/server_management.go` | 同步官方补丁归档 |
| `custom-addon/frontend/src/lib/schemas.ts` | 增加部署/回收响应 schema |
| `custom-addon/frontend/src/lib/queries.ts` | 增加部署/回收 mutation，并在成功后刷新记录与批次 |
| `custom-addon/frontend/src/routes/index.tsx` | 库存页新增目标下拉、`部署选中`、`回收选中` 和结果弹窗 |
| `custom-addon/docs/面板管理升级计划.md` | 同步 Phase 1-④ P0 状态 |

保留项：`/generate-quota` 端点未删除，当前内部复用 `deploy/local`；前端按钮已改为「兼容生成」。

## 九、2026-09-01 P1 实施记录

本轮继续完成 P1 的两个最小闭环：已检测筛选/统计、Token 刷新。

| 文件 | 改动 |
|---|---|
| `custom-addon/backend/data_records.go` | 列表请求新增 `detected=1` 筛选；统计响应新增 `detected`；列表响应新增 `token_exp`；新增 `RefreshDataRecordToken` 与 `UpdateTokensByEmail`，刷新成功回写 access/id/refresh token、`expired`、`last_refresh`，并清掉旧 `quota` / `nextTime` |
| `custom-addon/backend/data_records_test.go` | 增加已检测筛选、已检测统计、Token 刷新成功、`invalid_grant` 标记需重授权、按邮箱回写凭证字段、`token_exp` 解析测试 |
| `internal/api/server_management.go` | 注册 `POST /v0/management/data-records/refresh-token`，并把服务配置里的 `ProxyURL` 传给数据记录 Store |
| `custom-addon/modified-files/internal/api/server_management.go` | 同步官方补丁归档 |
| `custom-addon/frontend/src/lib/schemas.ts` | 增加 `detected`、`token_exp`、刷新响应 schema 与 `recordRefreshToken` |
| `custom-addon/frontend/src/lib/queries.ts` | 列表查询携带 `detected=1`；新增刷新令牌 mutation，成功/失败都会刷新记录缓存 |
| `custom-addon/frontend/src/store/app.ts` | 筛选状态新增 `detected` |
| `custom-addon/frontend/src/components/records/stats-cards.tsx` | 新增“已检测”统计卡，并联动筛选 |
| `custom-addon/frontend/src/routes/index.tsx` | 新增“刷新令牌”批量按钮；检测筛选结果会携带“已检测”筛选条件 |

## 十、2026-09-01 P2 与收尾实施记录

本轮补齐格式转换、重授权登记和 Phase 2 收尾。

| 文件 | 改动 |
|---|---|
| `custom-addon/backend/data_records.go` | 新增 `ConvertDataRecords`、`RegisterDataRecordReauth`；支持 `txt→CPA`、`CPA→sub2api`、`sub2api→CPA`；重授权登记按邮箱回写 token、`reset_expired_at`、`reauth_at`，并清空旧 `quota` / `nextTime` |
| `custom-addon/backend/data_records_test.go` | 增加三类转换测试、重授权更新/新增/缺字段计数测试；兼容生成验证 `deploy/local` 回写 |
| `internal/api/server_management.go` | 注册 `/v0/management/data-records/convert` 与 `/v0/management/data-records/register-reauth` |
| `custom-addon/frontend/src/routes/tools.tsx` / `index.tsx` | 新增工具页：输入上传、转换、下载、重授权产物上传；库存页新增「登记重授权」入口 |
| `custom-addon/frontend/src/lib/schemas.ts` / `queries.ts` | 增加转换与重授权响应 schema / API 调用 |
| `custom-addon/frontend/src/routes/__root.tsx` / `routeTree.gen.ts` | 顶部导航新增「工具」并更新路由树 |
| `custom-addon/docs/*` 与 `E:\ggbdpq\开发文档\06-卡密管理\卡密管理汇总.md` | 同步 8318、工具页、部署回收、重授权和 Chrome 扩展冻结状态 |

收尾事实：

- `C:\Users\802165\CLIProxyAPI\data\data-records.sqlite` 当前不存在，无需删除。
- `custom-addon/chrome-extension` 当前不存在，旧 Chrome 扩展形态保持冻结，不恢复。
- 本 fork 项目端口统一为 8318；仓库扫描不应再出现旧端口。

口径：`detected` 仍按旧项目定义，只表示记录已有 `quota` 和 `nextTime`，不等同于 `health=ok`。Token 刷新失败时，只有 `invalid_grant` 会把记录标为 `auth_state=needs_reauth`。
