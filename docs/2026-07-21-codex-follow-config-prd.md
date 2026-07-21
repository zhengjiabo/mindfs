# Codex「跟随配置」需求文档

## 1. 背景

MindFS 在 2026-07-20 引入 Codex「跟随配置」能力（commit `f8eadb7`）：当 Codex 未显式指定 model 时，应省略 model 覆盖，让 Codex 读取 `~/.codex/config.toml` 中的默认模型。

用户反馈：部分聊天窗口点击「跟随配置」后无效——原本停留在 `gpt-5.6-sol`，点完再打开选项仍停在 `gpt-5.6-sol`；另一些场景又能切换成功。

该问题属于「昨天刚加的功能」回归/实现缺陷，需要从第一性原理定义正确语义，再给出可验证修复方案。

## 2. 第一性原理

### 2.1 模型选择的三种语义

对 Codex 而言，模型来源只能是以下之一，且必须可区分：

| 语义 | 用户意图 | 持久化表示 | 运行时行为 |
|------|----------|------------|------------|
| **显式锁定（pin）** | 强制使用某模型，不受 config.toml 变更影响 | 非空 `model` | 请求时传入该 model |
| **跟随配置（follow）** | 不覆盖，使用 Codex 自身配置 | 空 `model`（unset） | 显式传递空 model override，由 Codex 读 config.toml |
| **未知/缺失** | 尚未选择（非 Codex 或历史脏数据） | 视 agent 而定 | 回退到 agent 默认或探测值 |

核心公理：

1. **空字符串是一等公民**，表示「follow」，不是「未设置需要回退」。
2. **空 ≠ 缺失**：`model === ""` 与 `model === undefined` / 未选择 在 Codex 下必须分开处理；UI 与状态机不得用 `value || fallback` 抹平空字符串。
3. **pin 与 follow 互斥**：用户点 follow 必须清除 pin；点具体模型必须建立 pin。
4. **三层状态一致**：UI 本地状态、session meta、user preferences 在 follow 切换后最终都要反映 unset。

### 2.2 状态分层

```
┌─────────────────────────────────────────────┐
│ L0 UI 草稿态  ActionBar.model / AgentSelector │  立即反映点击
├─────────────────────────────────────────────┤
│ L1 Session meta  session.Model               │  会话级 pin；空=该会话 follow
├─────────────────────────────────────────────┤
│ L2 User preference  preferences.json         │  全局默认；空=新建会话 follow
├─────────────────────────────────────────────┤
│ L3 Runtime indicator  current_model_id       │  只读展示 config.toml 当前值
└─────────────────────────────────────────────┘
```

- L3 **只读**：用于展示「当前配置: xxx」，**不可**写回 L0/L1/L2 当作用户选择。
- L2 影响**无 session / 新会话**的初始 L0。
- L1 影响**已有 session** 的 L0 同步。
- 用户点击「跟随配置」首先改 L0；发送消息后写 L1/L2 为空。

### 2.3 为何会出现「有时能切、有时不能」

同一 UI 入口，结果取决于 L2/L1 是否已有 pin：

| 场景 | L2 `default_model_id` | L1 `session.model` | 点击 follow 后（现状） | 用户感知 |
|------|----------------------|--------------------|------------------------|----------|
| 从未 pin 过 Codex | `""` | `""` 或无 session | L0 保持 `""` | 能切换 |
| 曾 pin `gpt-5.6-sol` | `gpt-5.6-sol` | 任意 | L0 被 `nextModel \|\| defaults.model` 回写成 sol | 无效 |
| 旧会话已 pin | 任意 | `gpt-5.6-sol` | 同上或发送后被 session 回灌 | 无效/闪一下又回去 |

**缺陷本质**：昨天的功能把空字符串定义成 Codex 的 follow 状态，但旧的“有值才使用、没值就回退”逻辑没有一起升级。结果是同一个空值在不同层被分别当成 follow、缺失或历史值，清除 pin 的操作被静默撤销。

当前至少存在四个需要一起处理的回填点：

1. `ActionBar` 的 `nextModel || defaults.model` 会把显式 follow 回填成全局 pin。
2. `AgentSelector` 的 `model || fallbackModel` 会让旧模型继续显示，甚至让 follow 与具体模型同时高亮。
3. `App` 发送消息前的 `effectiveModel` 计算会用当前 session 的旧 model 覆盖显式 follow。
4. 后端 `ensureAgentSession` 会从当前 session / 历史 exchange 推导旧模型；即使最终 meta 被清空，首次发送仍可能使用旧 pin。

## 3. 用户目标

1. 在任意 Codex 聊天窗口（新会话、已有会话、曾 pin 过模型）点击「跟随配置」后，选项高亮立即切到「跟随配置」。
2. 再次打开下拉，仍保持「跟随配置」，而不是回跳到 `gpt-5.6-sol` 等具体模型。
3. 发送消息后，该会话与全局默认均进入 follow 语义，后续轮次不再重新 pin。
4. 副文案展示 config.toml 当前模型（只读），例如「当前配置: gpt-5.6-sol」，不表示已 pin。
5. 再选具体模型时，重新 pin，行为与 follow 对称可逆。

## 4. 目标

1. 定义并统一 Codex model 的 pin / follow 语义（跨 UI、session、preferences、probe）。
2. 修复「点击跟随配置无效」的根因，覆盖曾 pin 与未 pin 全路径。
3. 保证 L0 点击即时正确；L1/L2 在发送消息后与 L0 一致。
4. 下拉高亮：follow 与具体模型互斥，不得双选。
5. 提供可回归的单测/组件行为说明，防止 `||` 再次抹平空串。

## 5. 非目标

1. 不在本次改变 Claude / 其他 agent 的默认回退逻辑（仍可用 `default || current`）。
2. 不实现「点击 follow 即立即写 preferences / session」（仍可在发送时落盘），但 L0 必须立刻正确。
3. 不改动 Codex config.toml 的读写本身。
4. 不做跨设备偏好同步。
5. 不在本需求中扩展「跟随配置」到非 Codex agent（除非后续统一抽象）。

## 6. 用户故事

### US-1 从 pin 切到 follow

- **Given** 当前 agent 为 codex，L0/L2 为 `gpt-5.6-sol`
- **When** 用户打开模型菜单并点击「跟随配置」
- **Then**
  - L0 `model` 变为 `""`
  - 「跟随配置」高亮；具体模型不高亮
  - 按钮 title 显示 `codex · 跟随配置`
  - 副文案可显示 config 当前模型

### US-2 再次打开仍保持

- **Given** US-1 完成后未刷新页
- **When** 关闭并再次打开模型菜单
- **Then** 仍高亮「跟随配置」

### US-3 发送后持久化

- **Given** US-1 完成
- **When** 用户发送一条消息
- **Then**
  - `session.Model == ""`
  - `preferences.agents.codex.model == ""`（字段可省略）
  - 后端不再把历史 exchange 模型写回 session
  - 后续消息请求不携带显式 model pin（或携带空，由 Codex 读 config）

### US-4 新会话继承 follow

- **Given** L2 已清空（follow）
- **When** 用户新建会话并选 codex
- **Then** L0 初始为 `""`，展示 follow，而不是 `current_model_id`

### US-5 从 follow 再 pin

- **Given** 当前为 follow
- **When** 用户点选 `gpt-5.6-sol`
- **Then** L0 为 `gpt-5.6-sol`；发送后 L1/L2 均为该值

### US-6 已有会话 pin 覆盖全局 follow

- **Given** L2 为 follow，但打开的旧会话 L1 为 `gpt-5.6-sol`
- **When** 同步 session 到 ActionBar
- **Then** L0 显示 sol（会话 pin 优先）
- **And** 用户可再点 follow 清除本会话 pin（发送后 L1 空）

## 7. 功能需求

### FR-1 空串是 follow 的合法值

所有 Codex 相关赋值路径：

- 不得使用 `nextModel || defaults.model`
- 不得使用 `model || current_model_id` 作为「用户选择」
- 允许：`nextModel !== undefined ? nextModel : defaults.model`（仅当调用方省略参数时）

### FR-2 选择器高亮规则（Codex）

- `agent === "codex" && model === ""` → 选中「跟随配置」
- `agent === "codex" && model === item.id` → 选中该模型
- 二者互斥

### FR-3 展示文案

- 按钮：`codex · 跟随配置`（follow）或 `codex · <model>`（pin）
- 菜单项副标题：有 `current_model_id` 时显示「当前配置: {id}」，否则 hint

### FR-4 后端语义（已有，需保持）

- `UpdateAgentDefaultsIfChanged("codex", "", ...)` 必须清空 model
- `ApplyAgentDefaults`：Codex 空 preference → `DefaultModelID = ""`，**不要**用 probe 默认填回
- `SendMessage`：`preferredModel = in.Model`；codex 且空时 `sessionModel = ""`
- 请求显式为空时，Codex 必须在当前 turn 前先清除 runtime model override；不能继续沿用当前 session pin
- 请求未携带 model 时，才允许按兼容规则继承当前 session；历史 exchange 只能作为展示/兼容数据，不能作为 Codex 的当前选择

### FR-5 即时反馈

点击 follow 后无需发送消息即可在 UI 看到选中态变化。

### FR-6 仅 Codex 展示 follow 入口

非 Codex agent 不显示「跟随配置」项（与现实现一致）。

## 8. 交互与边界

1. **无 agents 列表 / probe 失败**：仍可设置 L0 为空；副文案可无 config 模型。
2. **preferences 写失败**：不影响 L0；发送路径打日志，下次发送可重试。
3. **session 同步 effect**：从 session 灌入的 `model` 可以是 `""`；不得把 `""` 再替换成 default。
4. **effort / fast_service**：follow 时 effort 仍可按 config model 的 efforts 展示（可用 L3 只读模型的 efforts），不要求 model pin。
5. **任务模板 / 定时任务 / 插件会话**：若传入显式 model 则 pin；空则 follow。本次以聊天 ActionBar 为主路径，模板路径单独回归。

## 9. 验收标准

| ID | 用例 | 期望 |
|----|------|------|
| AC-1 | L2=sol 时点 follow | L0 立即 `""`，高亮 follow |
| AC-2 | AC-1 后重开菜单 | 仍高亮 follow |
| AC-3 | AC-1 后发第一条消息 | 发消息前 runtime override 已清空；preferences.model 空；session.model 空 |
| AC-4 | L2 已空时点 follow | 保持 follow，无异常 |
| AC-5 | follow 后点 sol | pin sol，高亮 sol |
| AC-6 | 旧会话 L1=sol，点 follow 并发消息 | L1 变空，后续轮次 follow |
| AC-7 | 单元测试：清空 preference model | 通过 |
| AC-8 | 无 `nextModel \|\| defaults.model` 类 Codex 路径 | 代码审查通过 |
| AC-9 | 请求未携带 model 与显式 `model: ""` | 前者继承、后者 follow，语义不混淆 |
| AC-10 | follow 后发送排队消息 | 队列保留 follow 意图，不恢复旧 pin |

## 10. 风险与依赖

- **风险**：其他调用 `onAgentChange(agent)` 省略第二参数时，若改为严格空串，可能把「未指定」误当 follow。需区分「显式传 `""`」与「未传」。
- **依赖**：`preferences.json`、session meta、agent probe 的 `default_model_id` / `current_model_id` 语义稳定。
- **兼容**：历史 preference 中已有 pin 的用户，升级后需能一点 clear；无需迁移脚本。

## 11. 成功度量

- 相关用户反馈「点跟随配置无效」归零。
- Codex 用户可在 pin ↔ follow 间任意往返，无「幽灵回跳」到上次 pin 模型。
