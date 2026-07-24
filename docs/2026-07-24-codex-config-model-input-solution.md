# Codex 配置模型快捷输入 — 技术解决方案

- 需求文档：`docs/2026-07-24-codex-config-model-input-prd.md`
- 相关既有能力：`docs/2026-07-21-codex-follow-config-*.md`、`docs/2026-07-24-codex-windows-follow-config-*.md`
- 设计原则：成熟小模式（行级配置补丁 + 受保护 API + 受控 UI 提交），不过度设计

## 1. 目标架构

```text
AgentSelector (Codex 模型子菜单)
  input + confirm button
       │ Enter / click
       ▼
web/src/services/agentConfig.ts
  setCodexConfigModel({ model })
       │ POST JSON
       ▼
HTTP protectedEndpoint
  POST /api/agent-config/codex-model
       │
       ▼
setCodexConfigModel(model)  [进程内 mutex]
  codexHomeDir()/config.toml
  patch top-level model only
       │
       ├─ respond { model, previous_model, changed }
       └─ triggerAgentConfigSwitchProbe("codex")
              └─ ClearProbeSession + async ProbeOne
                     └─ current_model_id 更新

前端成功后：
  onAgentChange("codex", "")  → ActionBar setModel("")  [?? 非 ||]
  fetchAgents(true)           → 刷新 current_model_id
  乐观更新 config 展示（可选）
```

## 2. 方案选择

| 方案 | 做法 | 评价 |
|------|------|------|
| A. 仅前端调外部 skill/脚本 | Web 无法可靠写服务器 home | 否 |
| B. 复用 API Provider switch | 会改 provider/base_url/token | 副作用过大 |
| C. 新专用 API 只改顶层 model | 最小权限、语义清晰 | **采用** |
| D. 通用 toml PATCH API | 过度设计 | 否 |

## 3. 后端设计

### 3.1 API

```http
POST /api/agent-config/codex-model
Content-Type: application/json

{ "model": "gpt-5.6-sol" }
```

成功 `200`：

```json
{
  "agent": "codex",
  "model": "gpt-5.6-sol",
  "previous_model": "grok-4.5",
  "changed": true
}
```

同值 no-op：

```json
{
  "agent": "codex",
  "model": "gpt-5.6-sol",
  "previous_model": "gpt-5.6-sol",
  "changed": false
}
```

错误：

| 条件 | HTTP |
|------|------|
| body 无效 | 400 |
| model 空/超长/含换行或 NUL | 400 |
| 无法解析 codex home | 500/400 |
| 读/写失败 | 500 |

路由注册于 `server/internal/api/http.go`，与其它 agent-config 路由并列，统一 `protectedEndpoint`。

### 3.2 核心算法：顶层 TOML 键补丁

新建纯函数（便于单测），建议落在 `server/internal/api/codex_config_model.go`（或同包 `agent_config.go` 旁）：

```go
func patchTopLevelTOMLStringKey(content, key, value string) (next string, previous string, changed bool, err error)
```

规则（对齐 skill `switch_model.py` + 现有 provider merge 的顶层处理）：

1. 统一扫描时容忍 `\r\n`；写回时若原内容含 `\r\n` 则输出 CRLF，否则 LF。
2. 只在**第一个 section 行**（`^\s*\[`）之前的顶层区域查找 `^\s*model\s*=`。
3. 匹配值支持双引号 / 单引号 / 裸值；写回统一为双引号转义字符串。
4. 找到则只替换 value span，保留 key 与间距、行尾注释（若注释在 value 后且解析不确定：**整行替换为 `model = "..."` 并丢弃该行尾注释**——更安全可预测；实现选整行替换顶层 model 行，避免半套 quote 解析 bug）。
5. 未找到则在第一个 section 前插入 `model = "..."\n`；若文件为空则写入该行。
6. section 内 `model =` 一律忽略。

**实现选择说明（刻意简单）**：对顶层 `model` 行采用「整行重写」而非 span 级保留注释。理由：model 行几乎从无行尾注释；完整 quote 状态机成本高。风险可接受。

### 3.3 路径解析

导出或复用 `agent.CodexHomeDir()`：

- 今日 `codexHomeDir()` 小写未导出 → **导出为 `CodexHomeDir`**（或新增 public wrapper），供 API 层调用。
- **不要**复制 `filepath.Join(os.UserHomeDir(), ".codex")` 到 API 包（避免与 `CODEX_HOME` 分叉；PRD FR-2.1）。

路径：`filepath.Join(agent.CodexHomeDir(), "config.toml")`。

若 home 为空：返回错误。

### 3.4 写盘与并发

```go
var codexConfigModelMu sync.Mutex

func setCodexConfigModel(model string) (previous string, changed bool, err error) {
  codexConfigModelMu.Lock()
  defer codexConfigModelMu.Unlock()
  // validate model
  // read file (missing file → treat as empty content, ensure dir)
  // patch
  // if !changed return
  // write with existing perm or 0o600
}
```

写盘后：

```go
triggerAgentConfigSwitchProbe(app, "codex")
```

**不**调用 `KillAgentProcess`（区别于 config backup switch / API provider switch）。

### 3.5 校验

```go
model = strings.TrimSpace(req.Model)
if model == "" || len(model) > 200 || strings.ContainsAny(model, "\n\r\x00") {
  return 400
}
```

## 4. 前端设计

### 4.1 Service

`web/src/services/agentConfig.ts` 新增：

```ts
export async function setCodexConfigModel(model: string): Promise<{
  agent: string;
  model: string;
  previous_model?: string;
  changed: boolean;
}> {
  return protectedJSON(appPath("/api/agent-config/codex-model"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ model }),
  });
}
```

### 4.2 AgentSelector UI

在 `submenuIsCodex` 分支、「跟随配置」按钮之后增加一行：

```text
[ text input                  ] [✓]
error message (optional)
```

状态：

- `configModelDraft: string`
- `configModelSaving: boolean`
- `configModelError: string`
- `configModelOverride: string | null`（乐观展示用，成功后设为 new model；agents 刷新后可清除）

提交逻辑 `submitConfigModel`：

1. trim draft；空 → set error，return
2. saving=true
3. `await setCodexConfigModel(draft)`
4. 成功：
   - clear draft/error
   - set optimistic config model
   - **不要**走 `handleAgentSelect`（它会关菜单）；改为：
     - `onAgentChange(submenuAgentStatus.name, "")`
     - 可选保持菜单打开，便于看到「当前配置」变化；或关闭——**推荐保持打开**直到用户点选其它项，但若现有结构难做，允许关闭，依赖下次打开正确性（PRD 允许）。
   - 触发 agents 刷新：通过新可选回调 `onAgentsRefresh?: () => void | Promise<void>`，由 ActionBar 注入 `async () => setAgents(await fetchAgents(true))`。
5. 失败：inline error
6. saving=false

键盘：

- `onKeyDown`: Enter → preventDefault + stopPropagation + submit（非 composing）
- Escape → clear draft/error，stopPropagation

确认按钮：`type="button"`，调用同一 submit。

### 4.3 「当前配置」展示

配置模型 ID 解析顺序：

```ts
const configModelId =
  configModelOverride ||
  submenuAgentStatus.current_model_id ||
  submenuAgentStatus.default_model_id ||
  "";
```

**必须修复/增强** `submenuConfigModel`：

- 先在 `models` 列表中 find；
- 若找不到但 `configModelId` 非空，**合成** `{ id: configModelId, name: configModelId }`；
- 禁止因 list 未包含自定义模型而让「当前配置」空白。

`configModelOverride`：

- 写入成功后设置；
- `submenuAgent` 切换时清空；
- 当 `agents` 中 codex.`current_model_id` 已等于 override 时清空（避免长期遮挡真实 probe）。

### 4.4 ActionBar 契约

现有：

```ts
setModel(nextModel ?? defaults.model);
```

空串 `""` 经 `??` 保留 —— **符合 follow**。实现时加注释禁止改回 `||`。

新增 refresh 回调传入 AgentSelector。

### 4.5 i18n

`zh-CN` / `en-US`：

- `agent.setConfigModelPlaceholder`
- `agent.setConfigModelSubmit`（按钮 aria/title）
- `agent.setConfigModelEmpty`
- `agent.setConfigModelFailed`（可带 `{error}`）

## 5. 测试计划

### 5.1 Go 单测（必做）

`patchTopLevelTOMLStringKey` / `setCodexConfigModel` 表测：

1. 替换已有顶层 model
2. 插入缺失 model（有 section 前）
3. 不修改 `[section]` 内 model
4. 保留其它顶层键与注释块
5. CRLF 输入 → CRLF 输出
6. 非法 model 报错
7. 同值 changed=false
8. `CODEX_HOME` 指向 temp dir 时写到正确路径

### 5.2 前端

- 以 typecheck 为主（按用户要求）。
- 不强制加 RTL 测试（仓库前端测试基础设施若薄弱则不加）。

### 5.3 手动验收清单

见 PRD §8。

## 6. 文件变更清单

| 文件 | 变更 |
|------|------|
| `server/internal/agent/discovery.go` | 导出 `CodexHomeDir` |
| `server/internal/api/codex_config_model.go` | 新建：patch + set + handler |
| `server/internal/api/codex_config_model_test.go` | 单测 |
| `server/internal/api/http.go` | 注册路由 |
| `web/src/services/agentConfig.ts` | `setCodexConfigModel` |
| `web/src/components/AgentSelector.tsx` | 输入行 + 提交 |
| `web/src/components/ActionBar.tsx` | 传入 refresh；必要时注释 `??` |
| `web/src/i18n/locales/zh-CN.ts` | 文案 |
| `web/src/i18n/locales/en-US.ts` | 文案 |
| `docs/2026-07-24-codex-config-model-input-*.md` | 本需求/方案 |

## 7. 实现步骤

1. 导出 `CodexHomeDir` + 纯函数 patch + 单测
2. HTTP handler + 路由 + probe 触发
3. 前端 service + i18n
4. AgentSelector UI + ActionBar 接线
5. `go test` 相关包 + `npm run typecheck`
6. 对抗式审查修复

## 8. 风险与缓解（方案层）

| 风险 | 缓解 |
|------|------|
| 整行替换丢掉 model 行尾注释 | 接受；model 行极少注释 |
| handleAgentSelect 关菜单导致看不到成功 | 成功路径可直接 onAgentChange；或关菜单也可接受 |
| agents 缓存 30s | 必须 `fetchAgents(true)` |
| probe 异步慢 | optimistic override |
| 导出 CodexHomeDir 影响面 | 仅改名导出，逻辑不变 |
| ActionBar `||` 回退 | 保持 `??`；审查时盯住 |
| 自定义模型不在 list → 当前配置空白 | 合成 ModelInfo 展示 |
| 仅改 L0 后 session effect 回弹 | 与点击「跟随配置」同级既有语义：发送前 L0 生效；session 文档 pin 在发送后落盘。不在本方案扩大 session 写穿 |

## 9. 非目标再确认

- 不写 effort
- 不 kill 业务 codex
- 不做模型自动完成
- 不改 provider

## 10. 开放问题

无。可进入开发。

## 11. 对抗式审查记录（实现阶段）

| 轮次 | 发现 | 处理 |
|------|------|------|
| R1 | 自定义 model 不在 list 时「当前配置」空白 | `submenuConfigModel` 合成 `{id,name}` |
| R1 | 未使用变量 `topLevelTOMLModelLine` | 删除 |
| R1 | 输入行与首个模型双边框 | 模型列表首项不再强制 topBorder |
| R2 | API 错误带 `invalid codex config model:` 前缀 | handler trim 后返回可读 message |
| R2 | Go 补丁/写盘/CODEX_HOME/CRLF/引号 | 单测覆盖并通过 |
| R3 | `??` 空串 follow 语义 | ActionBar 保持 `??` 并注释 |
| R3 | typecheck / api·agent 测试 | 通过 |

无开放问题。
