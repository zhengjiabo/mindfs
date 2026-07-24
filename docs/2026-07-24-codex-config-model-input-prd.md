# Codex 配置模型快捷输入 — 需求文档

## 1. 背景

MindFS 已支持 Codex「跟随配置」：

- UI 提供「跟随配置」选项；
- 会话侧 model override 为空时，不 pin 模型，由 Codex 读取 `~/.codex/config.toml`（或 `$CODEX_HOME/config.toml`）中的顶层 `model`；
- 模型菜单会展示当前配置模型（`current_model_id` / `agent.followConfigCurrent`）。

现有路径的缺口是：**修改 config.toml 中的默认模型** 仍依赖：

1. 人工编辑 `config.toml`，或
2. 在 Codex CLI / skill 中执行切换脚本，或
3. 通过「API Provider 切换」整包改写 provider + model（过重且副作用大）。

用户希望在 MindFS 聊天输入区旁的 **Agent/模型菜单** 内，直接输入模型名并写回配置文件，从而让「跟随配置」立刻指向新默认模型。

## 2. 第一性原理

### 2.1 两个正交概念

| 概念 | 存储位置 | 语义 | 影响范围 |
|------|----------|------|----------|
| **Session model override（pin / follow）** | MindFS 会话 L0/L1/L2、preferences | 非空 = pin 该模型；空 = follow | 当前会话 / 偏好 |
| **Config default model** | Codex `config.toml` 顶层 `model` | Codex 自身默认模型 | 所有 follow 的会话与新 Codex 会话 |

「跟随配置」只解决前者（清空 override）。本需求解决后者（改 config 默认值）。二者不能互相替代：

- 只 pin 会话模型 → config 不变，其它 follow 会话不受影响；
- 只改 config → 当前若仍 pin，会话不会自动 follow 新 config。

### 2.2 用户真正要的闭环

用户意图可还原为：

```text
输入模型名
  → 写入 config.toml 顶层 model
  → 当前选择切到「跟随配置」（空 override）
  → UI 显示新的「当前配置: <model>」
  → 后续 follow 发送不再携带 model pin
```

若只写 config 而不切到 follow，用户可能误以为「已经切换」，但当前会话仍 pin 旧模型。

### 2.3 为何放在模型菜单，而不是主聊天输入框

- 主聊天输入框语义是「发消息」；回车已绑定发送。
- 模型菜单已承载 model 选择、「跟随配置」与当前 config 展示；写 config 是同一决策面。
- 与现有「列表点选 pin」形成互补：列表点选 = pin；输入框确认 = 改 config 并 follow。

因此：**输入控件放在 Codex 模型子菜单内，紧邻「跟随配置」**，而不是 ActionBar 消息输入旁再加第二个全局输入框。

### 2.4 交互确认方式

用户明确允许：

- 可有按钮；
- 也可不要按钮，**回车确认**。

为降低误触与移动端可用性，采用：

- **Enter 确认**（桌面主路径）；
- **必有小型确认按钮**（移动端主路径 / 桌面辅助，同一提交逻辑）；
- Escape 清空草稿 / 取消编辑，不写文件。

不在输入过程中实时写文件。

## 3. 用户目标

1. 在 Codex 模型菜单中输入任意模型 ID，Enter 或点确认后写入 config 顶层 `model`。
2. 写入成功后，当前 UI 立即处于「跟随配置」，并展示新的配置模型。
3. 不必打开终端、不必手改 toml、不必切换 API Provider。
4. 失败时得到可读错误，不破坏原 config 文件。

## 4. 范围

### 4.1 In Scope

1. Codex agent 模型子菜单新增「写入配置模型」输入控件。
2. 后端 API：安全更新 Codex config 顶层 `model` 键（仅该键）。
3. 成功后：
   - 刷新 / 更新 Codex probe 状态中的 `current_model_id`（配置模型展示）；
   - 将当前会话选择设为 follow（`model = ""`）；
   - 不改 `model_provider`、effort、auth、projects 等其它配置。
4. 中英文文案。
5. 类型校验（前端 tsc；后端相关 Go 测试补齐）。

### 4.2 Out of Scope

1. 修改 `model_reasoning_effort` / service tier（除非后续单独需求）。
2. 非 Codex agent 的 config 写入。
3. 通过输入框 pin 会话模型但不写 config（列表点选已覆盖 pin）。
4. 校验模型是否存在于 provider 列表（允许用户写入自定义 / 尚未 list 到的模型 ID）。
5. 自动重启正在运行的 Codex 业务会话以强制旧 thread 换模。
6. 多设备 config 同步。
7. 可视化编辑整个 `config.toml`。
8. 通过本输入框 **unset/删除** 顶层 `model` 键。

## 5. 用户故事

### US-1 输入并写入配置

- **Given** 当前 agent 为 codex，模型菜单已打开
- **When** 用户在配置模型输入框输入 `gpt-5.6-sol` 并按 Enter（或点确认）
- **Then**
  - `config.toml` 顶层 `model` 变为 `gpt-5.6-sol`
  - 其它 toml 内容保持不变
  - UI 选择变为「跟随配置」
  - 「当前配置」文案显示 `gpt-5.6-sol`
  - 输入草稿被清空

### US-2 已 pin 时写入配置

- **Given** 当前会话 pin 了 `grok-4.5`
- **When** 用户输入 `gpt-5.4` 并确认写入配置
- **Then**
  - config 顶层 model = `gpt-5.4`
  - 当前选择从 pin 切到 follow（空 model）
  - 不再高亮 `grok-4.5`

### US-3 空输入

- **Given** 输入框为空或仅空白
- **When** 用户按 Enter / 点确认
- **Then** 不调用 API、不改文件；可轻微提示「请输入模型名」

### US-4 写入失败

- **Given** config 文件不可写或路径不可用
- **When** 用户确认写入
- **Then**
  - 展示错误信息
  - 原 config 内容不变
  - 当前 pin/follow 状态不变

### US-5 非 Codex

- **Given** 当前子菜单 agent 不是 codex
- **When** 打开模型列表
- **Then** 不显示该输入控件

### US-6 移动端仅按钮

- **Given** 移动端打开 Codex 模型菜单
- **When** 输入模型名并点确认按钮（无物理 Enter）
- **Then** 与桌面 Enter 行为一致

## 6. 功能需求

### FR-1 UI 位置与形态

1. 仅当 submenu agent 为 `codex` 时渲染。
2. 位于「跟随配置」选项附近（紧随其后，作为附属行）。
3. 单行 text input：
   - placeholder：`写入配置模型` / `Set config model`
   - hint/title 可补充「回车或点按钮确认；写入 ~/.codex 默认模型并跟随」。
4. Enter 触发提交；IME 组合输入（`isComposing` / keyCode 229）期间 Enter 不提交。
5. **必须**提供右侧确认按钮，与 Enter 共用同一提交函数。
6. 提交中 disable 输入与按钮，防止重复提交。
7. 打开模型菜单时**不**自动 focus 该输入框。
8. 成功提交后清空输入草稿；菜单开闭跟随现有 `onAgentChange` 行为，但再次打开时 follow 与「当前配置」必须正确。
9. 输入框内按键不得冒泡成菜单级快捷键误触发。
10. 失败时在输入行附近展示 inline 错误文案（不强制全局 toast）；成功可不 toast，依赖「当前配置」文案变化。

### FR-2 写入语义

1. 目标文件解析**必须**复用与 probe/discovery 相同的单一实现（`codexHomeDir()` 语义：`CODEX_HOME` 优先，否则 `UserHomeDir()/.codex`），**禁止**再写一份只认 `~/.codex` 的分叉路径。
2. 仅更新**顶层** `model = "..."`：
   - 已存在 → 原地替换值；
   - 不存在 → 在第一个 `[section]` 之前插入；
   - 不得改写 section 内的 `model =`；
   - 不得全文 TOML re-serialize（保留注释与无关键）。
3. 不修改：`model_provider`、`model_reasoning_effort`、`model_providers.*`、projects、auth 等。
4. 模型名：只 trim 首尾空白；中间字符原样保留；不做 inventory 校验。
5. 拒绝：空串、含 `\n`/`\r`/`\x00`、长度 > 200。
6. 换行风格：原文件以 CRLF 为主则写回 CRLF，否则 LF。
7. 文件权限：已存在则保留原 perm；新建 `0o600`（Windows 上 Unix 位可被忽略）。
8. **不支持** unset 顶层 `model`。
9. 若写入值与当前顶层 `model` 相同：视为成功 no-op（不报错），仍执行 FR-3 切 follow 与刷新，保证用户手势闭环一致。

### FR-3 成功后的状态机

写入 API 成功后，前端必须：

1. `onAgentChange(codex, "")` —— 切到 follow（不得用 `|| fallbackModel` 把空串打回旧 pin）；
2. 强制刷新 agents（`fetchAgents(true)` 或等价）；
3. probe 未完成前允许乐观更新 config model 展示。

后端必须：

1. 写盘成功后清 probe session 并异步 ProbeOne（复用 `triggerAgentConfigSwitchProbe` 一类逻辑）；
2. **不**强制 Kill 业务会话池中的 Codex 进程。

### FR-3.1 生效范围

| 对象 | 期望 |
|------|------|
| UI「当前配置」 | 写盘 + probe 后更新；允许短暂乐观值 |
| 当前 ActionBar 选择 | 立即 follow（空 override） |
| **新** follow 发送 / 新 thread | 应读到新 config model |
| **已 pin** 的其它会话 | 保持原 pin |
| **已存在 follow 会话** 若 runtime 缓存旧 defaults | 不保证旧 thread 热更新；可通过重开会话或重启 agent 对齐。本需求不强制全局 restart |

### FR-4 与列表操作关系

| 操作 | config.toml | session override |
|------|-------------|------------------|
| 点「跟随配置」 | 不变 | `""` |
| 点具体模型 | 不变 | 该 model id |
| 输入框确认 | 顶层 model 更新 | `""`（follow） |

### FR-5 安全与并发

1. API 走 `protectedEndpoint`。
2. 请求体限大小；模型名 ≤ 200。
3. 拒绝 NUL/换行，防止破坏 toml。
4. 读→改→写；进程内 mutex 串行化该文件更新。
5. 多进程 last-writer-wins（不做跨进程文件锁，与现有 provider 切换同级）。
6. 响应只返回 old/new model 与逻辑标识（如 `codex-config`），不回传全文、不强制暴露 home 绝对路径给前端 UI。

### FR-6 可观测性

服务端日志：agent、old→new、成功/失败。不记录完整文件内容。

### FR-7 回归约束

不得破坏既有 follow-config 修复：Codex 空 model 是合法 follow 状态；前端/后端不得用 `|| defaults.model` 吞空串。

## 7. 非功能需求

1. 最小改动：复用 agent-config/agents API 风格与 AgentSelector 样式。
2. 不过度设计：无通用 toml 编辑器、无强制自动完成。
3. 跨平台路径与 discovery 一致。

## 8. 验收标准

1. Codex 模型菜单可见输入框+确认按钮；其它 agent 不可见。
2. 输入模型并确认 → 顶层 model 变更，UI follow，展示新配置模型。
3. 列表外自定义模型 ID 可写入。
4. 空输入 → 无文件变更。
5. 非法模型名 → 4xx，文件不变。
6. section 内其它 `model =` 不被改动；注释保留。
7. 刷新后 `/api/agents` 中 Codex `current_model_id` 与新值一致（允许短暂异步）。
8. 点「跟随配置」/ 点具体模型 pin 行为与改前一致。
9. 移动端仅按钮可完成写入。
10. `web` typecheck 通过；相关后端单测通过。
11. 不启动 dev、不要求完整打包。

## 9. 风险与对策

| 风险 | 级别 | 对策 |
|------|------|------|
| 误以为输入框是 pin | 中 | 文案「写入配置」；成功强制 follow |
| toml 注释/格式破坏 | 中 | 顶层 key 行级替换，不 re-serialize |
| 与 API Provider 切换互相覆盖 | 中 | 只碰顶层 `model` |
| probe 延迟 | 低 | 乐观更新 + force fetch |
| 旧 pin 会话不切换 | 低（预期） | FR-3.1 |
| 模型不存在导致后续发送失败 | 低（接受） | 不做 inventory 强校验 |
| 并发写 | 低 | 进程内 mutex；跨进程 LWW |
| CODEX_HOME 分叉 | 高→已关 | 强制 `codexHomeDir()` 语义 |
| 移动端无 Enter | 中→已关 | 确认按钮必有 |
| 旧 thread 缓存 | 中→已关 | FR-3.1，不强制 kill |
| CRLF 被改写 | 中→已关 | 保留换行风格 |
| 回退 follow-config 空串修复 | 中→已关 | FR-7 + 验收 8 |
| 无法 unset model | 低 | out of scope |

## 10. 开放问题

无。已拍板：

1. 控件在 **Codex 模型子菜单**，非主消息输入框旁。
2. **Enter + 必有确认按钮**。
3. 成功 = **写 config + 切 follow**。
4. 不改 effort / provider。
5. 不校验 list 库存。
6. 不支持 unset。
7. 不强制重启业务会话；见 FR-3.1。

## 11. 成功度量（定性）

- 用户可在约 5 秒内从模型菜单完成「改默认模型并跟随配置」。
- 日常换模无需再手改 `config.toml`。
