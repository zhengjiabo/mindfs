# Codex「跟随配置」技术解决方案

## 1. 关联文档

- 需求文档：`docs/2026-07-21-codex-follow-config-prd.md`
- 引入功能的提交：`f8eadb7 fix: let Codex follow config.toml when model is unset`

## 2. 结论先行

问题不是 Codex 配置读取失败，而是“空模型”的语义在链路中被反复抹掉。

昨天的提交已经把空字符串定义为 Codex 的 follow 状态，但没有同步修改所有依赖 falsy 回退的旧代码。一个选择从 UI 到运行时的路径中，至少有四次机会把 `""` 变回 `gpt-5.6-sol`：

```text
选择器 -> ActionBar 状态 -> App 发消息参数 -> WS/队列 -> 后端 runtime -> session/preferences
   ""          || fallback       || session.model       历史 exchange / current model
```

推荐采用“显式选择”和“未提供选择”分离的契约：

| 传输状态 | 值 | 含义 |
|---|---|---|
| 未提供 | `undefined` / 没有 `model` 字段 | 继承已有 session 选择，兼容旧调用方 |
| 显式 follow | `model: ""` | Codex 清除 model override，读取 `config.toml` |
| 显式 pin | `model: "gpt-5.6-sol"` | Codex 锁定指定模型 |

持久化层仍只保存一个字符串：非空表示 pin，空表示 follow。只有在 API 边界增加“字段是否出现”的信息，才能同时支持兼容继承和显式清除。

## 3. 现状与故障复现

### 3.1 功能提交改变了什么

`f8eadb7` 做了几件正确的事情：

1. Codex 未显式选择模型时，`DefaultModelID` 保持为空。
2. `preferences.json` 中 Codex 空 model 会清除旧 pin。
3. 发送后 Codex session model 可以被写为空。
4. `resolveRuntimeModel` 的发送后解析已移除历史 exchange 回填。

但旧代码的默认假设仍是“空值表示没有选择，应当回退”：

- `web/src/components/ActionBar.tsx`：`setModel(nextModel || defaults.model)`
- `web/src/components/AgentSelector.tsx`：当前 agent 的 `model || fallbackModel`
- `web/src/App.tsx`：`effectiveModel` 多处使用 `|| session.model`
- `server/internal/api/usecase/session.go`：另一条更早执行的 runtime 初始化路径仍从当前 session / 历史 exchange 推导模型

这是一类语义迁移缺陷：新增了一个合法状态，却没有审查所有“空值回退”边界。

### 3.2 用户案例的实际时序

假设全局 preference 和旧 session 都是 `gpt-5.6-sol`：

```text
1. 打开菜单
   model = "gpt-5.6-sol"

2. 点击「跟随配置」
   AgentSelector 回调 nextModel = ""

3. ActionBar 接收回调
   setModel(nextModel || defaults.model)
   => model 又变成 "gpt-5.6-sol"

4. 即使第 3 步被修掉，菜单计算仍可能执行
   targetModel = model || fallbackModel
   => 旧模型继续被视为选中

5. 即使菜单显示被修掉，发送前仍可能执行
   effectiveModel = model || session.model
   => 显式 follow 被重新变成旧 session pin

6. 即使请求终于带着空 model 到达后端
   ensureAgentSession 仍可能把 current.Model / historical exchange 当成目标
   => 当前第一轮仍使用旧 runtime override
```

因此“从未 pin 的场景能切换、曾经 pin 的窗口不能切换”完全符合现有代码行为。

## 4. 第一性原理与不变量

### 4.1 分开三个概念

当前实现用同一个 `model` 字符串承载三个不同概念，导致回退混乱。修复后必须在命名和流程上分开：

1. **Model override（用户选择）**：空表示 follow，非空表示 pin。
2. **Effective runtime model（实际执行模型）**：可能来自 `config.toml`，只用于运行结果和展示。
3. **Historical exchange model（历史记录）**：只能描述过去，不能决定当前选择。

`current_model_id` 是 runtime/config 的只读指示器；`default_model_id` 是用户 preference 应用后的 override 指示器。两者不应互相写回。

### 4.2 必须成立的状态不变量

1. Codex 的 `model === ""` 是合法且可持久化的 follow 状态。
2. 显式 `model: ""` 的优先级高于当前 session 的非空 model。
3. 未提供 `model` 与显式空 model 的语义不同。
4. UI 中 follow 和具体模型至多有一个被选中。
5. 显式 follow 的第一轮请求前，runtime 的 model override 必须已清空。
6. 历史 exchange 永远不能把 Codex 从 follow 恢复成 pin。
7. 非 Codex agent 的既有空值回退行为不因本修复改变。
8. 排队消息必须携带原始 model 选择语义，而不是只携带格式化后的显示值。

## 5. 推荐方案

### 5.1 前端选择器：保留显式空值

#### `ActionBar.tsx`

两处规则需要改变：

```ts
// 只在调用方没有提供第二个参数时使用默认值。
setModel(nextModel ?? defaults.model);
```

发送聊天消息时，Codex follow 必须传递空字符串，而不是转换成 `undefined`：

```ts
// chat: model 可以是 ""，该字段必须保留
onSendMessage(payload, mode, agent, model, ...);
```

命令模式可继续传 `undefined`，因为命令不使用模型选择。

#### `AgentSelector.tsx`

当前 submenu 正在展示的 agent 时，使用父组件传入的 model 原值；不能用 `||` 回退：

```ts
function selectedModelId(
  status: AgentStatus,
  activeAgent: string,
  activeModel: string,
): string {
  if (status.name === activeAgent && status.name === "codex") {
    return activeModel; // "" 就是 follow
  }
  if (status.name === activeAgent) {
    return activeModel || status.default_model_id || status.current_model_id || "";
  }
  return status.name === "codex"
    ? status.default_model_id || ""
    : status.default_model_id || status.current_model_id || "";
}
```

高亮规则固定为：

```text
codex + model==""       => follow 高亮，具体模型全部不高亮
codex + model==modelID  => 对应模型高亮，follow 不高亮
```

`current_model_id` 只出现在 follow 的副文案“当前配置: xxx”中，不能参与选择状态计算。

### 5.2 App 发送路径：区分 absent 与 empty

`handleSendMessage` 保持 `model?: string` 的类型，但用 `model !== undefined` 判断字段是否由调用方提供：

```ts
const modelSpecified = model !== undefined;

// 当前交互会话：显式空值必须保留
effectiveModel = modelSpecified
  ? model!
  : effectiveAgent === previousAgent
    ? session.model || ""
    : "";
```

只有“没有提供 model”时才继承目标 session。不要使用：

```ts
effectiveModel = model || session.model || "";
```

发送服务保持空字符串字段：

- `model: ""`：序列化后字段存在，后端识别为显式 follow。
- `model: undefined`：JSON 中字段省略，后端识别为 inherit。

### 5.3 WebSocket 与队列：传递字段存在性

在 `server/internal/api/ws.go` 增加带存在性返回值的解析函数：

```go
func getOptionalString(payload map[string]any, key string) (string, bool, error)
```

建议行为：

- 字段不存在：`("", false, nil)`
- 字段为字符串（包括空串）：`(value, true, nil)`
- 字段存在但类型错误：返回请求错误，不静默当作空串

将结果写入 `SendMessageInput.ModelSpecified`，同时写入 `PendingUserMessage.ModelSpecified`。队列的冻结、复制、提升和立即发送路径都必须保留该布尔值。

`ModelSpecified` 是内部传输元数据，不需要暴露为用户可编辑字段；如队列状态要跨进程持久化，则应把它加入持久化 schema，而不是依赖空字符串推断。

### 5.4 后端：目标 override 与运行时状态分离

将当前的 `resolveRuntimeModel` 拆成语义明确的目标选择函数，例如：

```go
func resolveRequestedModelOverride(
    agentName string,
    current *session.Session,
    requested string,
    specified bool,
) string
```

规则：

```text
Codex + specified=true  + requested=""       => ""      (显式 follow)
Codex + specified=true  + requested="model"  => "model" (显式 pin)
Codex + specified=false + current.Model=pin   => pin     (兼容继承)
Codex + specified=false + current.Model=""   => ""      (follow)
其他 agent + requested 非空                     => requested
其他 agent + requested 为空且未显式指定           => 保持既有回退逻辑
```

在 `ensureAgentSession` 中：

1. 使用上述函数得到 desired override。
2. 若已有 runtime session，使用 `existing.CurrentModel()` 比较 runtime 当前 override。
3. 不使用 `resolveSessionExchangeModel(current)` 决定当前 Codex runtime model。
4. desired override 为空时，调用 `existing.SetModel(ctx, "")`，必须发生在发送 turn 之前。
5. 新建/恢复 runtime session 时将空 model 原样放入 `OpenSessionInput`。

历史 exchange 仍可用于旧 agent 的兼容回退，但不应参与 Codex follow 的当前状态决策。

### 5.5 偏好与 session 持久化

`preferences.Store` 需要能够区分“model 不变”和“model 清空”。推荐把更新参数改为 patch：

```go
type AgentDefaultsPatch struct {
    Model       *string // nil=不修改；指向空串=follow/清除 pin
    Effort      string
    FastService string
}
```

这样可以避免某次请求没有携带 model、但只修改 effort 时意外清除 Codex pin。

发送完成后的规则：

- `ModelSpecified=true`：把 `Model`（包括空串）写入 preferences 和 session。
- `ModelSpecified=false`：不修改 model preference/session；只按既有规则处理其他设置。
- Codex 空 model 的 session exchange 记录为空，不能再从历史记录推导旧 pin。

已有 `ApplyAgentDefaults` 逻辑基本正确，应保留：Codex 空 preference 必须得到空 `DefaultModelID`，不能被 probe 的 `CurrentModelID` 填回。

### 5.6 内部调用方

统一检查以下生产入口：

- WebSocket 聊天：由 payload 字段存在性决定。
- 看板 Agent stage：stage 配置本身是显式选择，`Model: ""` 表示 follow，应设置 `ModelSpecified=true`。
- 定时任务：任务配置字段是显式选择，空值同样表示 follow。
- 排队消息：沿用入队时的 `ModelSpecified`。
- slash command：模型不是命令语义的一部分，保持现状或明确设置为未指定。

## 6. 关键时序

### 6.1 从 pin 切换到 follow 并发送

```text
用户点击 follow
  -> AgentSelector 发出 (model="")
  -> ActionBar 保留 ""
  -> 菜单只高亮 follow

用户发送消息
  -> App 不回退到 session.model
  -> WS 收到 model 字段，ModelSpecified=true
  -> ensureAgentSession desired override=""
  -> runtime.SetModel("")
  -> Codex turn 读取 config.toml
  -> UpdateModel(session, "")
  -> preferences patch.Model=&""
  -> 广播 session / agent status 更新
```

### 6.2 未提供 model 的兼容请求

```text
请求没有 model 字段
  -> ModelSpecified=false
  -> 若已有 session pin，继承 pin
  -> 不清除 preferences/session
```

这条路径避免旧客户端或非模型相关调用因为空字符串而意外清除用户选择。

## 7. 文件级改动清单

### 必改

1. `web/src/components/ActionBar.tsx`
   - 将 model 选择回退从 `||` 改为 presence-aware / `??`。
   - 聊天发送保留空 model。
2. `web/src/components/AgentSelector.tsx`
   - 当前 Codex 的空 model 不得回退到 `default_model_id`。
   - 修正 follow 与具体模型的互斥高亮。
3. `web/src/App.tsx`
   - `effectiveModel` 使用 `undefined` 判断继承。
   - 不用 session.model 覆盖显式空值。
4. `server/internal/api/ws.go`
   - 解析 model 字段存在性。
   - 将 `ModelSpecified` 传入 usecase 和队列。
5. `server/internal/api/stream_hub.go`
   - 复制/提升队列项时保留 `ModelSpecified`。
6. `server/internal/api/usecase/session.go`
   - 引入 desired override 解析。
   - follow turn 前清除 runtime override。
   - 不用历史 exchange 决定 Codex 当前模型。
7. `server/internal/preferences/store.go`
   - 支持 model patch 的 nil/空串区分。

### 需要同步核查

1. `server/internal/api/appcontext.go`
2. `server/internal/scheduled/tasks.go`
3. `server/internal/api/usecase/usecase_test.go`
4. `server/internal/api/ws_test.go`
5. `web/src/components/TaskTemplateDialog.tsx`
6. `web/src/components/ScheduledAgentTaskDialog.tsx`

## 8. 测试方案

### 8.1 Go 单元测试

增加表驱动测试覆盖：

| 场景 | current.Model | specified | requested | 期望 desired override |
|---|---|---:|---|---|
| Codex 显式 follow | `sol` | true | `""` | `""` |
| Codex 显式 pin | `""` | true | `sol` | `sol` |
| Codex 兼容继承 | `sol` | false | `""` | `sol` |
| Codex 已 follow | `""` | false | `""` | `""` |
| Claude 既有行为 | `sonnet` | false | `""` | `sonnet` |

另外覆盖：

- preference patch `nil` 不修改 model。
- preference patch 指向空串会清除旧 pin。
- 现有 runtime pin + 显式 follow 会调用一次 `SetModel("")`。
- 历史 exchange 有 `sol`、session model 为空时，Codex 不恢复 sol。
- 入队、出队、立即发送保留 `ModelSpecified=true`。
- 非 Codex 既有 model 选择测试不回归。

### 8.2 前端验证

当前 `web` 没有现成组件测试脚手架，不为一个窄修复引入新的测试框架。先做：

- `yarn typecheck`
- `yarn build`
- 代码搜索确认 Codex 选择路径不存在 `nextModel || defaults.model`、`model || fallbackModel`、`effectiveModel || session.model` 形式的语义回退。

然后做浏览器冒烟矩阵：

1. 新会话、全局 pin：pin -> follow -> 关闭/重开菜单。
2. 已有 session pin：follow -> 首次发送 -> 检查 runtime 和 session meta。
3. follow -> pin -> follow 往返。
4. 有历史 exchange pin、session model 为空的旧会话。
5. follow 后发送排队消息、点击“立即发送”。
6. 定时任务与任务模板选择 Codex follow。
7. Codex probe 失败 / `current_model_id` 为空。
8. Claude 选择模型、发送、切换行为。

检查 WebSocket 帧时必须看到 follow 请求包含：

```json
{"model":""}
```

而不是完全没有 `model` 字段。

## 9. 对抗式审查

### 审查问题 1：只修 `nextModel || defaults.model` 是否足够？

不够。菜单自身还有 `model || fallbackModel`，发送路径还有 `effectiveModel || session.model`，后端首次 turn 还会继承旧 runtime。只修一行会造成“看起来能点，但发送后又回去”。

### 审查问题 2：把所有 `||` 都机械替换成 `??` 是否安全？

不安全。`??` 只能修复“空串是合法值”的地方；如果调用方根本没有表达“字段是否提供”，后端仍无法区分继承和显式清除。必须在传输边界保留 presence bit。

### 审查问题 3：发送后清空 session 是否足够？

不够。runtime session 在发送前已经用旧 pin 打开，第一轮仍可能错误执行。清除 runtime override 必须发生在 turn 前。

### 审查问题 4：历史 exchange 是否可以作为 fallback？

不可以。exchange 是过去的事实，不是当前用户选择。否则 follow 后每一轮都会从旧 exchange 找回 pin。

### 审查问题 5：`current_model_id` 与 pin 恰好相同怎么办？

不能靠模型 ID 文本判断。选择器必须以 override 状态决定高亮，并把 config 当前模型放在 follow 副文案中。

### 审查问题 6：排队消息会不会恢复旧状态？

会，除非把字段存在性随队列保存。队列不能只传空字符串，因为 JSON/旧结构可能再次把它当缺失。

### 审查问题 7：非 Codex 会不会被误改？

这是高风险回归点。follow 语义只对 Codex 生效；非 Codex 仍按旧规则处理空值。相关解析函数必须带 agent 名称，不能做全局空值清除。

### 审查问题 8：旧客户端不发送 model 字段怎么办？

按 inherit 处理，不清除既有 pin。这是兼容性优先的安全默认。新 Web 客户端必须显式发送空字段才能执行 follow。

### 审查问题 9：preferences 写失败怎么办？

runtime/session 仍可以完成当前 follow；记录结构化错误并通过 agent status 变化刷新。下一次新会话可能继续看到旧 preference，这是可观测的持久化失败，不应悄悄将 UI 恢复成 pin。

### 审查问题 10：用户快速 follow -> pin -> follow 怎么办？

发送时只使用最后一次已提交的 UI 状态；点击回调不能异步读取旧闭包。应在浏览器冒烟中验证连续操作，必要时给选择状态加 revision，而不是读取 probe 的 current model 反推。

## 10. 不采用的方案

### 方案 A：只改一行 `||` 为 `??`

改动最小，但无法解决菜单计算、发送 fallback、后端首次 turn 和历史 exchange 四个问题，拒绝。

### 方案 B：使用特殊字符串 `"__follow_config__"`

能绕过空值问题，但会把 UI 哨兵值泄漏到 session、日志、模型校验和第三方 agent，增加数据污染风险，拒绝。

### 方案 C：点击菜单后立刻调用后端写 preferences/session

会增加网络竞态、失败回滚和多窗口同步复杂度，且不能替代发送前清除 runtime override。本次不采用；仍以发送时落盘为主。

### 方案 D：全链路改成 tagged union

长期最干净，例如 `inherit | follow | pinned(id)`，但会波及 session schema、任务模板、定时任务和多个 API。本次先以 presence bit + 空字符串持久化完成兼容修复，并把 tagged union 作为后续模型选择重构方向。

## 11. 可观测性与发布

保留并补充结构化日志：

```text
[session/model] selection agent=codex specified=true requested="" desired="" source=explicit-follow
[session/model] switch.detected ... from="gpt-5.6-sol" to="" action=set_runtime_model
[preferences] agent_defaults.update.done agent=codex model=""
```

发布顺序：

1. 先合入后端兼容 presence bit，未升级客户端仍按 inherit 工作。
2. 再发布前端，确保 follow 请求包含 `model: ""`。
3. 观察旧 pin 用户的首次 follow turn、preferences 更新错误和队列路径日志。
4. 验证无异常后再考虑清理历史兼容代码。

不需要数据迁移。已有非空 Codex preference 仍表示 pin，用户第一次点击 follow 后清空即可。

## 12. 完成定义

以下条件全部满足才算完成：

1. AC-1 至 AC-10 全部通过。
2. Codex pin -> follow 的第一轮实际请求不再携带旧 model override。
3. session、preferences、UI 高亮三者最终一致。
4. 排队消息和任务/定时任务路径不重新引入空值回填。
5. Go 测试、前端 typecheck/build 通过。
6. 对抗式审查中列出的十类反例均有代码或测试覆盖。
