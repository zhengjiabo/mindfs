# 任务会话可识别标题技术解决方案

## 1. 关联需求

需求文档：`docs/2026-07-12-task-session-title-prd.md`

目标是把任务会话名称从：

```text
流程 / #14
```

调整为：

```text
#14 · 安装 image-to-code 技能
```

同时保证命名失败不影响任务执行、任务编号始终保留、用户手动改名不会被异步结果覆盖。

## 2. 现状分析

### 2.1 任务会话创建

任务 Agent 阶段通过 `AppContext.EnsureAgentSession()` 创建会话：

```text
server/internal/api/appcontext.go
```

当前名称只使用：

1. `Task.TaskTemplateName`
2. `Task.TaskNumber`

因此同一模板的任务只能显示成“流程 / #编号”。

### 2.2 普通聊天自动命名

普通聊天在首条消息发送后异步调用：

```text
usecase.Service.SuggestSessionName()
```

该流程具备：

1. 独立命名会话；
2. 30 秒超时；
3. AI 精炼标题；
4. 失败不影响主会话；
5. 会话元数据更新广播。

但它目前只允许在会话名称仍等于 `BuildFallbackSessionName(firstMessage)` 时改名。任务会话使用自定义初始名称，因此不能直接复用。

## 3. 总体设计

采用“确定性初始标题 + 异步 AI 精炼”的两阶段方案：

```text
任务提示词
  → 提取具体任务内容
  → 创建 #编号 · 本地截断摘要
  → 立即启动任务 Agent
  → 异步调用已有 AI 命名
  → 名称未被修改时写回 #编号 · AI 摘要
```

主任务执行和标题生成互不阻塞。

## 4. 后端设计

### 4.1 提取任务标题来源

在 `server/internal/api/appcontext.go` 增加纯函数：

```go
func taskSessionTitleSource(prompt string) string
```

处理顺序：

1. 去除 `Task control context:` 及其后内容；
2. 查找最后一个位于行首（允许前导空格）的 `任务：` 或 `任务:`；
3. 标记后存在非空内容时，只返回该部分；
4. 否则返回清理后的完整提示；
5. 保留原始语义，空白压缩交给现有标题工具处理。

这里不使用复杂自然语言规则猜测主题，避免把“页面分享”“英雄评分”等任务错误截断成本身无意义的首个短语。

### 4.2 创建初始标题

增加纯函数：

```go
func taskSessionInitialName(task kanban.Task, prompt string) (name, source, prefix string)
```

规则：

1. `source` 为提取后的具体任务内容；
2. 本地摘要调用现有 `usecase.BuildFallbackSessionName(source)`；
3. 有任务编号时前缀为 `#<number> · `；
4. 有摘要时名称为 `prefix + summary`；
5. 摘要为空时回退到原有模板名与编号组合。

初始标题不等待模型，确保会话创建后立即可识别。

### 4.3 扩展通用自动命名输入

扩展 `usecase.SuggestSessionNameInput`：

```go
type SuggestSessionNameInput struct {
    RootID       string
    SessionKey   string
    Agent        string
    FirstMessage string
    ExpectedName string
    NamePrefix   string
}
```

兼容规则：

1. `ExpectedName` 为空时，仍以 `BuildFallbackSessionName(FirstMessage)` 作为允许自动改名的名称，普通聊天行为不变；
2. `ExpectedName` 非空时，仅当当前名称与其完全相等才允许改名；
3. AI 返回值清理后再拼接 `NamePrefix`；
4. 拼接后的最终标题为空或与当前名称相同则不写入。

最终写回通过 session manager 的原子条件改名完成：只有持锁读取到的当前名称仍等于 `ExpectedName` 才更新。该扩展避免复制整套命名逻辑，也消除了“检查名称后、AI 返回前用户手动改名”的竞态窗口。

### 4.4 异步精炼任务标题

`EnsureAgentSession()` 创建任务会话并广播初始元数据后，启动 goroutine：

```go
SuggestSessionName({
    FirstMessage: source,
    ExpectedName: initialName,
    NamePrefix:   prefix,
})
```

成功后复用 `BroadcastSessionMetaUpdated()` 更新右侧列表。

错误只记录日志，不返回给任务调度器。

### 4.5 会话复用

如果任务已经存在主会话，`EnsureAgentSession()` 会在创建逻辑之前直接返回已有 session key，不重复命名。

同阶段复用同理。只有真正创建新任务会话时才触发标题生成。

## 5. 前端设计

不需要修改前端。

右侧会话列表已经响应会话元数据更新事件，并直接展示后端保存的 `session.Name`。后端广播异步改名结果后，列表会自动刷新。

## 6. 测试方案

### 6.1 标题来源提取测试

覆盖：

1. 固定背景 + 流程 + 中文 `任务：`；
2. 英文冒号 `任务:`；
3. 多个任务标记时取最后一个；
4. 自动追加 `Task control context:` 时正确移除；
5. 无任务标记时使用完整提示；
6. 标记后为空时安全回退。

### 6.2 初始名称测试

覆盖：

1. 编号与摘要组合为 `#14 · ...`；
2. 无编号时只显示摘要；
3. 无摘要时回退模板名与编号。

### 6.3 通用自动命名兼容测试

对拆出的纯函数或判断函数覆盖：

1. 普通聊天未传 `ExpectedName` 时行为不变；
2. 任务会话传入 `ExpectedName` 时使用指定名称保护；
3. `NamePrefix` 正确保留任务编号；
4. 空 AI 输出不生成只有编号的异常名称。

### 6.4 回归测试

执行：

```bash
go test ./server/internal/api/...
go test ./server/internal/kanban/...
```

如仓库全量测试耗时可接受，再执行：

```bash
go test ./server/...
```

## 7. 失败与降级策略

| 场景 | 行为 |
|---|---|
| 标题 Agent 不可用 | 保留本地初始标题 |
| 标题 Agent 超时 | 保留本地初始标题 |
| AI 返回空字符串 | 保留本地初始标题 |
| 用户提前手动改名 | 检测名称不等于 `ExpectedName`，放弃写回 |
| 任务内容无 `任务：` | 使用完整提示生成摘要 |
| 任务内容为空 | 回退到“模板名 / #编号” |
| 元数据广播失败 | 数据库名称仍已更新，刷新后可见 |

## 8. 兼容性与迁移

1. 不涉及数据库结构变更。
2. 不修改已有会话名称。
3. 新字段只用于内部 Go 结构，不影响 HTTP/WS 协议。
4. 普通聊天调用方不设置新增字段，行为保持不变。

## 9. 可观测性

沿用现有日志：

```text
[session-name] suggest.error
[session-name] rename.error
[session-name] rename.done
```

任务侧异步调用增加上下文日志，包含 root、task 和 session，便于区分标题失败与任务执行失败。

## 10. 对抗式审查结论

### 10.1 “只用本地规则，不调用 AI”

否决。长模板中的真实任务可能是多句、编号列表或后置条件，本地截断只能作为可靠兜底，无法稳定生成用户期望的语义摘要。

### 10.2 “直接复用普通聊天命名，不增加 ExpectedName”

否决。普通聊天只允许覆盖首条消息截断形成的默认名；任务初始标题包含编号，直接调用不会生效。去掉名称校验又会覆盖用户手动改名。

### 10.3 “同步等待 AI 标题后再运行任务”

否决。标题不是任务执行前置条件，同步等待会让批量任务启动增加最多 30 秒延迟，并放大命名服务故障影响。

### 10.4 “AI 直接返回带编号的完整标题”

否决。编号是系统事实，不应交由模型生成；由后端添加前缀可避免编号遗漏、幻觉或格式不一致。

### 10.5 剩余风险

AI 精炼会增加轻量调用成本，但普通聊天已经采用同一机制。异步执行、本地兜底和失败隔离使该风险可接受。当前没有需要用户决策的开放问题。
