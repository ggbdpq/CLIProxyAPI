# CLIProxyAPI 数据管理面板 —— Chrome 插件

在浏览器端为 CLIProxyAPI 管理面板（`http://localhost:8317/management.html`）注入「数据管理」页，**无需服务端做前端注入**（替代 `updater.go` / `server_management.go` 的 Go 注入逻辑，启动服务时设 `CPA_PLUGIN_UI=1` 即剥离服务端注入）。

## 工作原理

- 插件用 `content_scripts` 注入 `content.css`（样式）+ `content.js`（页面逻辑），`world: "MAIN"`。
- 脚本在管理面板页面的**主世界**运行：可读取页面已保存的管理密钥（localStorage / sessionStorage），同源 `fetch` 无 CORS 限制。
- 侧边栏新增「数据管理」入口，功能与原 `data_management_extension.html` 完全一致：JSONL 导入 / 导出 / 列表 / 搜索 / 分页 / 删除 / 批量生成配额。

## 后端要求（重要）

**Chrome 插件只替代前端注入层。** 后端服务仍需包含数据管理 API，二者缺一不可：

| 后端组件 | 文件（参考模块内） | 能否由插件替代 |
|---|---|---|
| 数据管理 handler + SQLite | `custom-addon/backend/data_records.go`（编译进二进制） | ❌ 必须在 Go 源码中 |
| 5 条 `/v0/management/data-records*` 路由 | `modified-files/internal/api/server_management.go` | ❌ 必须在 Go 源码中 |
| Codex 配额回写 | `modified-files/internal/api/handlers/management/api_tools.go` | ❌ 必须在 Go 源码中 |
| 前端注入 | （`updater.go` / `server_management.go` 的 Go 注入部分，设 `CPA_PLUGIN_UI=1` 可跳过） | ✅ 由本插件替代 |

如果后端源码**没有**上述 API，插件注入的 UI 会报「请求失败 / 404」。请确保使用的是带数据管理后端（`data_records.go`）的构建。

## 安装

1. 打开 Chrome，地址栏进入 `chrome://extensions/`
2. 右上角打开「开发者模式」
3. 点「加载已解压的扩展程序」，选择本目录：`custom-addon/chrome-extension/`
4. 打开管理面板 http://localhost:8317/management.html ，左侧应出现「数据管理」入口

## 使用

1. 在「数据管理」页输入管理密钥（`Authorization: Bearer <key>` 所需），密钥会存入当前标签页的 sessionStorage，刷新后仍保留。
2. 点「导入 JSONL」选择文件 → 写入本地 `data/data-records.sqlite`。
3. 列表支持搜索、分页、勾选删除、勾选导出、勾选生成配额。

## 端口 / URL 匹配

manifest 仅匹配 `localhost:8317` 与 `127.0.0.1:8317` 的 `management.html`。8318 是 Go 注入版（未设 `CPA_PLUGIN_UI`），**故意不匹配**，避免重复注入。若服务端口不同，请编辑 `manifest.json` 的 `matches` 数组：

```json
"matches": [
  "http://localhost:8317/management.html",
  "http://127.0.0.1:8317/management.html"
]
```

后端必须包含数据管理 API（`custom-addon/backend` 已编译进二进制即可），并以 `CPA_PLUGIN_UI=1` 启动（`scripts/start-8317.bat` 已内置）——否则服务端注入与插件注入并存（幂等标记会避免重复渲染），缺数据管理后端时插件 UI 会 404。

## 兼容与注意

- 需要 Chrome 111+（`content_scripts.world: "MAIN"` 自该版本起支持）。
- **8318 是 Go 注入版（未设 `CPA_PLUGIN_UI`），插件不匹配该端口，互不干扰**。若手动把 matches 扩展到 8318，与 Go 注入版同时生效会重复绑定事件监听（UI 不会重复——两者共享幂等标记 `window.__cpaDataMgmtInjected`，后注入者跳过渲染）。
- 插件不采集、不外发任何数据；密钥只经由页面自身存储与逻辑处理。
- `content.js` 与 `content.css` 是从 `frontend/data_management_extension.html` 提取生成，修改功能时**两处需同步**。
