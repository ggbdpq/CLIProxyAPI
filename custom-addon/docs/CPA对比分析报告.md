对比分析已完成，先给你完整结论，然后就"跑起来"确认一个关键前提。

## 一、改动对比结论

你的源码 `cpa-manage-tools` 是**基于官方 v7.2.128（2026-08-10）之前某版本 fork** 的（本地 908/1043 个源码文件与官方 v7.2.128 完全一致）。官方最新 HEAD（v7.2.129，8月12日）又新增了 20+ 提交的其它功能。

### "数据管理"功能的改动 = 5 个新增文件 + 7 个修改文件

**新增文件（5 个）：**

| 文件                                                         | 作用                                                         |
| ------------------------------------------------------------ | ------------------------------------------------------------ |
| [internal/api/handlers/management/data_records.go](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/internal/api/handlers/management/data_records.go) (864行) | 后端核心。JSONL 数据记录导入/导出/列表/删除/配额生成/更新，通过**内嵌 node:sqlite 脚本**操作本地 `data/data-records.sqlite` |
| [internal/api/handlers/management/data_records_test.go](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/internal/api/handlers/management/data_records_test.go) | 对应测试                                                     |
| [internal/managementasset/data_management_extension.html](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/internal/managementasset/data_management_extension.html) (542行) | 前端。管理面板新增"数据管理"页：列表/搜索/分页/导入/导出/删除/批量生成配额 |
| [config.yaml](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/config.yaml) | 本地运行配置（当前端口 **8317**）                            |
| [使用指南.md](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/使用指南.md) | 说明文档                                                     |

**修改文件（7 个）：**

| 文件                                                         | 改动点                                                       |
| ------------------------------------------------------------ | ------------------------------------------------------------ |
| [internal/api/server_management.go](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/internal/api/server_management.go) | 注册 5 条 `/v0/management/data-records*` 路由；下载管理面板时用 `InjectDataManagementExtension()` 注入扩展 |
| [internal/managementasset/updater.go](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/internal/managementasset/updater.go) | `//go:embed data_management_extension.html` + 新增 `InjectDataManagementExtension()` 注入函数 |
| [internal/api/handlers/management/api_tools.go](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/internal/api/handlers/management/api_tools.go) | 新增 `syncCodexQuotaNextTimeFromAPICall()`：Codex 账号请求 `/wham/usage` 后自动解析配额重置时间/剩余百分比，回写数据记录 |
| [internal/managementasset/updater_test.go](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/internal/managementasset/updater_test.go) | 新增注入测试                                                 |
| [internal/api/handlers/management/api_tools_test.go](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/internal/api/handlers/management/api_tools_test.go) | 新增配额同步测试                                             |
| [config.example.yaml](vscode-webview://086md48o61jrftk1b66eg255ijimphep3pmjtk5pmheevj3crmgv/cpa-manage-tools/config.example.yaml) | **默认端口改为 8318**（官方是 8317）、auth-dir 相对路径、fill-first 策略 |
| `.gitignore`                                                 | 忽略 `data/`、`logs/`、`.cli-proxy-api/` 等运行时目录        |

其余 122 个差异文件（executor/translator/signature 等）是**官方在你这版之后的版本演进**（is-compat、realtime 路由重构、kimi validation、think-replay 等），不是你的改动。

```
✶ Insight ─────────────────────────────────────
```

- 数据管理功能采用"后端 Go handler + 内嵌 Node 脚本 + SQLite"三层：Go 只做 HTTP 边界，SQLite 操作全部委托给 `node:sqlite`（从 node v24.14 起内建），通过 stdin/stdout 传 JSON。这样避免了引入 CGO 的 SQLite 依赖，但**运行时必须有支持内建 sqlite 的 node**。
- 前端注入用 `//go:embed` + 字符串拼接到 `</body>` 前，标记 `cpa-data-management-extension`，并在 `updater.go` 里做幂等防重注入——这是不动官方 `management.html`（会从远端拉取更新）的轻量扩展方式。`─────────────────────────────────────────────────`