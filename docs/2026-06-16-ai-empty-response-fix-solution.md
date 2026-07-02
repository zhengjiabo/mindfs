# AI 空回复误判与错误透传修复技术方案

## 1. 背景

近期用户侧偶发看到“AI 没有返回内容”一类提示。生产日志显示，这类现象主要发生在 Codex 会话中，并不等价于“模型完全没有被调用”。实际链路中已经出现过以下几类事件：

1. Codex 上游返回明确错误，例如 `429 Too Many Requests`、`413 Payload Too Large`。
2. Codex app server 在上下文压缩或中断恢复阶段返回协议错误，例如 `tool_choice` 参数非法、`no active turn to interrupt`。
3. Codex SDK 产生了计划、diff、工具调用等事件，但没有产生可持久化的 `message_chunk` 文本。

当前 MindFS 的会话持久化逻辑只把 `message_chunk` 拼入 assistant 正文。于是当一轮对话有工具事件、计划事件或错误事件，但没有自然语言文本 chunk 时，后端可能落一条空 assistant 记录，前端再显示成“AI 没有返回内容”。

## 2. 现状链路

### 2.1 Codex 事件接入

Codex 会话通过 `RunStreamed()` 获取流式事件：

- `server/internal/agent/codex/session.go:153`
- `server/internal/agent/codex/session.go:187`

当前已显式处理的事件包括：

1. `ItemStartedEvent` / `ItemUpdatedEvent` / `ItemCompletedEvent`
2. `TurnCompletedEvent`
3. `TurnFailedEvent`
4. `ThreadErrorEvent`
5. 少量 raw event，例如 `thread.tokenUsage.updated`

其中 assistant 文本只来自 `AgentMessageItem.Text` 的增量：

- `server/internal/agent/codex/session.go:207`
- `server/internal/agent/codex/session.go:219`

未处理的 raw event 会被记录为 `unhandled.raw_event`，例如 `turn.plan.updated`、`turn.diff.updated` 等。

### 2.2 后端会话持久化

会话发送逻辑在 `server/internal/api/usecase/session.go` 中完成。当前关键行为如下：

1. 只有 `EventTypeMessageChunk` 会写入 `responseText`。
   - `server/internal/api/usecase/session.go:1314`
2. 收到工具调用、工具更新、todo、thought 等事件时，只更新辅助记录或上下文状态，不写入 assistant 正文。
   - `server/internal/api/usecase/session.go:1275`
   - `server/internal/api/usecase/session.go:1290`
3. 如果 `sendErr != nil` 且本轮没有看到 assistant chunk，只打印 `turn.send.no_response`，但后续仍会继续持久化 user 和 agent 记录。
   - `server/internal/api/usecase/session.go:1338`
   - `server/internal/api/usecase/session.go:1340`
   - `server/internal/api/usecase/session.go:1395`
4. 最后才把 `sendErr` 返回给 WebSocket 层。
   - `server/internal/api/usecase/session.go:1409`

这意味着：在“出错且没有任何文本 chunk”的场景下，当前代码可能先写入空 assistant，再把错误返回给外层。

### 2.3 WebSocket 错误透传

WebSocket 层会把 `SendMessage` 返回的错误广播给前端：

- `server/internal/api/ws.go:605`
- `server/internal/api/ws.go:608`

错误事件类型是：

```text
session.stream -> error
```

错误文案由 `normalizeAgentErrorMessage()` 归一化：

- `server/internal/api/ws.go:861`

### 2.4 前端流式状态

前端 `useSessionStream()` 收到 `error` 或 `message_done` 后会停止流式状态，但会清空 `streamStatusText`：

- `web/src/hooks/useSessionStream.ts:394`
- `web/src/hooks/useSessionStream.ts:405`

如果持久化会话里已经存在空 assistant 记录，SessionViewer 层最终只能按“没有正文内容”展示兜底提示，真实上游错误容易被掩盖。

## 3. 根因判断

本问题不是单点问题，而是三层行为叠加：

1. **上游真实失败没有被优先持久化为错误态**
   - Codex 调用确实可能失败。
   - 当前后端先落空 agent 记录，再返回错误。

2. **`message_chunk` 被当作唯一“AI 有回复”的判据**
   - 对普通聊天回复，这个判据成立。
   - 对工具型任务、文件修改任务、计划/diff 型事件，这个判据不完整。

3. **前端没有稳定展示本轮最终错误**
   - 流式 error 事件存在，但状态文本会被清空。
   - 历史会话刷新后，空 assistant 记录比临时错误事件更容易成为最终可见结果。

## 4. 目标

本次修复目标：

1. 不再把“出错且无文本 chunk”的回合持久化成空 assistant。
2. 前端优先展示真实上游错误，而不是泛化为“AI 没有返回内容”。
3. Codex 计划、diff、工具事件不再被误判成“无回复”。
4. 会话历史刷新后仍能保留错误信息或明确的非文本回复状态。
5. 不扩大修改面，不重写现有会话渲染结构。

## 5. 非目标

本方案不处理以下事项：

1. 不解决上游模型本身的 429/413/协议错误。
2. 不重构 Codex SDK 或 app server。
3. 不把所有 raw event 都完整渲染成 UI 组件。
4. 不改变 Claude / ACP 的正常回复语义，除非共用会话持久化分支必须调整。

## 6. 修复方案

### 6.1 后端：错误优先持久化

当 `sendErr != nil && !isCanceledTurnError(sendErr)` 时，后端应先进入错误处理分支，再决定是否持久化 assistant。

建议规则：

1. user 消息仍然持久化。
2. 如果本轮已有 `responseText`，保留 partial assistant，再返回错误。
3. 如果本轮没有 `responseText`，不要写入空 assistant。
4. 将错误作为结构化辅助事件或 assistant 错误文本持久化，二选一。

推荐优先方案：新增 `ExchangeAux` 错误记录。

理由：

1. 不污染 assistant 正文。
2. 前端可以明确区分“AI 文本回复为空”和“本轮调用失败”。
3. 后续可以对错误态增加重试按钮。

建议新增字段：

```go
type ExchangeAux struct {
    ...
    Error *ExchangeError `json:"error,omitempty"`
}

type ExchangeError struct {
    Message string `json:"message"`
    Agent   string `json:"agent,omitempty"`
    Code    string `json:"code,omitempty"`
}
```

如果短期不想扩展 session schema，可先采用折中方案：当无文本且有错误时，把 agent 记录写成明确错误文本，例如：

```text
调用失败：<normalized error message>
```

但该方案会把错误混入 assistant 正文，后续迁移成本更高，只适合作为临时补丁。

### 6.2 后端：区分“无文本但有有效事件”

在 `SendMessage` 聚合过程中增加一个布尔量：

```go
sawRenderableEvent := false
```

以下事件都应视为本轮有有效输出：

1. `EventTypeMessageChunk`
2. `EventTypeToolCall`
3. `EventTypeToolUpdate`
4. `EventTypeTodoUpdate`
5. `EventTypeThoughtChunk`

规则：

1. `sawAssistantChunk == true` 表示有文本回复。
2. `sawRenderableEvent == true` 表示本轮有可展示活动。
3. `!sawAssistantChunk && sawRenderableEvent && sendErr == nil` 不应提示“AI 没有返回内容”。
4. `!sawAssistantChunk && !sawRenderableEvent && sendErr == nil` 才是真正的空响应。

对于“只有工具/diff，没有最终文本”的成功回合，应持久化一个非文本完成标记，避免前端刷新后丢失本轮完成状态。

### 6.3 Codex 适配：处理 plan/diff raw event

当前 Codex raw event 中的 `turn.plan.updated`、`turn.diff.updated` 已出现在生产日志，但未映射成内部事件。

建议分两步处理：

1. `turn.plan.updated`
   - 映射成现有 `EventTypeTodoUpdate` 或新增 `EventTypePlanUpdate`。
   - 短期优先复用 `EventTypeTodoUpdate`，减少前端改造。

2. `turn.diff.updated`
   - 映射成 `ToolCall` 类型，`Kind=edit`，`Status=running/complete`。
   - `Content` 中保存 diff 文本，`RawType=turn.diff.updated`。

这样可以让“写文件但没有自然语言总结”的回合在 MindFS 里仍然是可展示、可追踪的有效输出。

### 6.4 WebSocket：错误事件带 requestId 和持久化线索

当前错误事件只有 message：

```go
Data: map[string]string{"message": errorMessage}
```

建议扩展为：

```go
Data: map[string]string{
    "message": errorMessage,
    "requestId": requestID,
    "agent": agentName,
}
```

如果采用 `ExchangeAux.Error`，还应确保错误事件和持久化错误记录使用同一份归一化消息。

### 6.5 前端：错误态优先展示

前端需要避免把 error 事件清空后只剩“空内容”兜底。

建议：

1. `useSessionStream()` 新增 `streamErrorText`。
2. 收到 `event.type === "error"` 时保存 `event.data.message`。
3. `message_done` 不清掉最近错误，除非同一 request 已产生有效 assistant chunk。
4. SessionViewer 对空 assistant 的兜底顺序调整为：
   - 有持久化 error：展示错误
   - 有工具/计划/diff 辅助事件：展示工具/计划/diff
   - 以上都没有：展示“AI 没有返回内容”

## 7. 推荐落地顺序

### Phase 1：止血

目标：真实错误不再被空 assistant 覆盖。

改动：

1. `SendMessage` 中当 `sendErr != nil && responseText == ""` 时，不写空 agent 正文。
2. 先写 user 消息，再返回错误。
3. WebSocket error 事件保留真实错误。
4. 前端 error 事件展示优先级高于空内容兜底。

验证：

1. 构造 agent 返回错误，确认不会新增空 assistant。
2. UI 展示真实错误文案。

### Phase 2：有效非文本事件识别

目标：工具型任务不再误判为空回复。

改动：

1. 增加 `sawRenderableEvent`。
2. 对工具、todo、thought 事件持久化完成状态。
3. 历史刷新后仍能看到本轮工具输出或计划输出。

验证：

1. 构造只有工具调用、无最终文本的会话。
2. 确认不会显示“AI 没有返回内容”。

### Phase 3：Codex raw event 映射

目标：Codex 计划和 diff 成为一等流式事件。

改动：

1. `turn.plan.updated` 映射成 todo/plan update。
2. `turn.diff.updated` 映射成 edit tool update。
3. 将未处理 raw event 的日志降噪，只保留真正未知类型。

验证：

1. 触发 Codex 修改文件任务。
2. 前端可看到计划、diff 或编辑工具卡片。
3. 会话完成后没有空 assistant。

## 8. 测试计划

### 8.1 后端单元测试

新增用例：

1. agent 在发送阶段直接返回错误，且没有任何 chunk。
   - 期望：不持久化空 assistant。
   - 期望：返回真实错误。

2. agent 先输出 partial chunk，随后返回错误。
   - 期望：保留 partial assistant。
   - 期望：返回真实错误，并可触发 recovery。

3. agent 只有 tool call / tool update，最终成功。
   - 期望：不生成“空内容”错误。
   - 期望：辅助事件被持久化。

4. agent 什么都没有输出但正常 done。
   - 期望：标记为真正空响应。

相关测试文件可放在：

- `server/internal/api/usecase/usecase_test.go`
- `server/internal/agent/codex/session_test.go`

### 8.2 前端测试

新增或补充测试：

1. 收到 `session.stream error` 后展示真实错误。
2. 空 assistant 且有错误记录时，不展示“AI 没有返回内容”。
3. 空 assistant 但有 tool/diff 辅助记录时，展示工具/差异内容。
4. 真正无任何输出时，才展示“AI 没有返回内容”。

### 8.3 手工回归

手工验证场景：

1. 触发 429 或模拟 agent 返回错误。
2. 触发大上下文导致 413。
3. 让 Codex 执行只改文件、少自然语言总结的任务。
4. 刷新页面后重新打开会话。
5. 移动端 App / WebView 下查看同一会话。

## 9. 风险与兼容

### 9.1 Session schema 兼容

如果新增 `ExchangeAux.Error`，旧会话不受影响。前端读取时按可选字段处理即可。

### 9.2 前端展示兼容

如果短期内前端还不识别错误辅助记录，后端仍会通过 WebSocket error 事件展示当前轮错误。但刷新后可能丢失错误态，因此前后端应一起上线。

### 9.3 Agent 差异

Claude / ACP 也复用会话持久化逻辑。修改 `SendMessage` 的错误持久化分支时，需要确认：

1. Claude 正常文本流不受影响。
2. ACP 工具事件不会被错误识别为空响应。

## 10. 验收标准

修复完成后应满足：

1. 上游返回 429/413/协议错误时，界面展示真实错误。
2. 不再出现“真实错误 + 空 assistant 覆盖”的历史记录。
3. Codex 工具型任务即使没有自然语言总结，也不会被标记成 AI 无返回。
4. 刷新页面后，错误态或工具态仍可见。
5. 后端日志中 `turn.send.no_response` 数量明显下降；剩余命中应能对应真正空响应。

## 11. 建议优先级

建议按 P0 / P1 划分：

P0：

1. 错误优先持久化。
2. 不写空 assistant。
3. 前端展示真实 error。

P1：

1. `sawRenderableEvent`。
2. Codex `turn.plan.updated` / `turn.diff.updated` 映射。
3. 错误辅助记录 schema。

P2：

1. 错误重试按钮。
2. 更完整的 Codex raw event UI。
3. 针对 429/413 的用户友好建议文案。

