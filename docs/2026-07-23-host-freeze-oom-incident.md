# 2026-07-23 主机卡死与 OOM 事故复盘

## 1. 摘要

2026-07-23，MindFS 所在的腾讯云主机出现严重卡顿并最终需要重启。排查确认，主机长期存在内存压力，并在同日上午发生过一次有完整内核记录的全局 OOM。故障期间表现为系统负载和 CPU 指标异常升高，但主要矛盾不是用户态计算打满，而是物理内存与 swap 被耗尽后形成的换页和 I/O 等待风暴。

本次排查还发现：MindFS 在长时间 Codex 工具调用期间会保留流式事件；普通 Codex 命令更新携带完整累计输出，而流事件缓存没有按调用合并或设置总量上限。这条链路与 `mindfs` 主进程增长到约 1.8 GiB 的现象高度吻合，但由于生产进程当时没有开启 heap profile，只能将其定性为高置信度根因，而不是已经由 Go heap profile 完成逐对象证明的结论。

另外存在数项放大故障或制造噪声的运维问题：前端类型检查会瞬时占用约 1 GiB 内存和两个 CPU 核心；Claude Code Router 和反向 SSH tunnel 都存在 systemd 重启循环；Stargate cron 有重复入口且锁的使用方式不正确。

## 2. 影响范围

- 主机：腾讯云 CVM，4 vCPU
- 物理内存：约 3.6 GiB
- swap：约 1.9 GiB
- 主要服务：MindFS、Codex app-server、OpenClaw、Docker 工作负载、PM2 服务
- 直接影响：主机失去响应、MindFS 被 OOM killer 终止、systemd-journald watchdog 失败、需要人工重启主机

## 3. 结论先行

### 3.1 已证实结论

1. 主机发生过全局 OOM，不是单纯 CPU 计算打满。
2. 2026-07-23 08:43，2 GiB swap 仅剩 224 KiB，系统负载达到 `162.75`。
3. 同一采样点用户态 CPU 只有 `0.60%`，系统态 CPU 为 `30.99%`，I/O wait 达到 `66.63%`。
4. 内核杀掉了 `mindfs` 主进程；该进程当时匿名 RSS 约为 `1,806,824 KiB`。
5. 过去多次停止 MindFS 时，systemd 记录到该 unit 的内存峰值为 `2.2–2.9 GiB`，并使用过 `1.0–1.3 GiB` swap。
6. 前一天也发生过全局 OOM，并杀掉了 `openclaw-gateway`；这不是一次孤立事件。
7. 14:48 左右上一轮系统日志突然终止，15:09 重新启动，没有正常关机序列，符合主机卡死后被强制重启的表现。

### 3.2 高置信度根因判断

主要故障链路为：

```text
长时间 Agent/Codex 任务
  -> 工具执行不断产生累计输出
  -> MindFS 保留未完成回合的流式事件和完整工具状态
  -> mindfs 主进程匿名内存持续增长
  -> RAM 与 swap 耗尽
  -> kswapd/直接回收、磁盘换页与进程调度延迟
  -> load average 和 iowait 急升
  -> cron、构建任务等并发活动继续放大压力
  -> OOM killer / journald watchdog / 整机失去响应
```

CPU 占用升高是故障过程中的症状和放大器，但不是这次事故的第一原因。

## 4. 时间线

时间均为 Asia/Shanghai。

### 4.1 历史内存异常

| 时间 | 事件 |
|---|---|
| 07-16 00:39 | 全局 OOM，`mindfs.service` 中的 Node 进程被杀。 |
| 07-18 20:45 | 正常停止 MindFS，systemd 记录 unit 峰值内存 2.9 GiB、swap 峰值 1.0 GiB。 |
| 07-20 22:10 | 正常停止 MindFS，峰值内存 2.5 GiB、swap 峰值 1.3 GiB。 |
| 07-21 13:34 | 正常停止 MindFS，峰值内存 2.5 GiB、swap 峰值 1.2 GiB。 |
| 07-22 12:14 | 全局 OOM，`openclaw-gateway` 被杀，同时 journald watchdog 失败。 |
| 07-22 16:47 | 正常停止 MindFS，峰值内存 2.2 GiB、swap 峰值 1.2 GiB。 |

这些记录说明服务运行一段时间后反复逼近整机内存上限。

### 4.2 2026-07-23 上午 OOM

| 时间 | 事件 |
|---|---|
| 07:58:51 | MindFS 中一个 Codex 会话收到用户指令“执行”。 |
| 08:02–08:09 | 同一 Codex thread 多次出现 `item.commandExecution.terminalInteraction`，回合没有正常 `output.done`。 |
| 08:43:06 | systemd 触发内存分配时进入 OOM killer。 |
| 08:43:08 | sysstat 记录 load average 162.75/163.57/136.16，14 个阻塞任务。 |
| 08:43:08 | swap 总量约 1.94 GiB，仅剩 224 KiB。 |
| 08:43:10 | 内核杀掉 `mindfs`，匿名 RSS 约 1.8 GiB。 |
| 08:43:10 | `systemd-journald` watchdog 失败并重启，journal 被标记为损坏或未正常关闭。 |
| 08:43:21 | systemd 自动重新启动 MindFS。 |
| 08:55 | 原会话重新导入外部增量并恢复执行。 |

sysstat 在故障点的 CPU 数据：

```text
%usr=0.60  %sys=30.99  %iowait=66.63  %idle=1.36
```

这组数据表明机器大部分时间消耗在内核内存回收和 I/O 等待，而不是业务代码进行正常计算。

### 4.3 下午卡死与重启

| 时间 | 事件 |
|---|---|
| 14:40 | 最近一次 sysstat 采样仍显示 CPU 约 95% idle。 |
| 14:48 | 上一轮 journal 最后记录，之后没有正常关机或 kernel panic 记录。 |
| 15:09 | 系统重新启动。 |

由于 14:48 到 15:09 之间没有可用遥测，无法证明下午卡死就是某一个具体进程再次触发 OOM；但上午已经确认的 OOM、长期内存峰值以及重启后的实时资源尖峰共同表明，内存压力仍是最主要的系统性风险。

### 4.4 重启后的实时复现信号

排查过程中捕获到 `/root/xiaozhen` 的 `vue-tsc --noEmit`：

- 瞬时 RSS 约 1.0 GiB
- CPU 约 188%，即接近占满两个 CPU 核心
- 执行结束后内存和 CPU 很快恢复
- 同期 MindFS cgroup 峰值达到约 2.6 GiB

这说明前端类型检查本身可以制造明显资源尖峰。它不等于内存泄漏，但在只有 3.6 GiB RAM、同时运行 Codex/OpenClaw/Docker 的主机上，足以把已有内存压力推向 swap 和 OOM。

## 5. MindFS 代码层分析

### 5.1 Codex 命令更新携带完整累计输出

`server/internal/agent/codex/session.go` 的 `mapToolItem` 在处理 `CommandExecutionItem` 时，直接把 `AggregatedOutput` 放入工具调用内容：

```go
if v.AggregatedOutput != nil && strings.TrimSpace(*v.AggregatedOutput) != "" {
    content = append(content, types.ToolCallContentItem{
        Type: "text",
        Text: *v.AggregatedOutput,
    })
}
```

如果 SDK 在每次 `ItemUpdatedEvent` 中返回“截至当前的完整输出”，每次事件都可能再次携带之前已经出现过的全部内容。

### 5.2 StreamHub 对普通工具更新逐条追加

`server/internal/api/stream_hub.go` 的 `AppendReplyEvent` 会把事件直接追加到 `ReplyingList`：

```go
state.ReplyingList = append(state.ReplyingList, cloneEvent(event))
```

目前只有 `source=userShell` 的流式事件会按 `CallID` 合并，并有 256 KiB 上限。普通 Codex `commandExecution` 更新没有同等的合并和总量限制。

因此，一个持续输出的命令可能在内存中形成类似以下序列：

```text
更新 1: 保存 1 MiB
更新 2: 保存累计 2 MiB
更新 3: 保存累计 3 MiB
...
```

总内存增长可能远大于命令最终输出大小，并接近二次增长。

### 5.3 未完成回合延长对象生命周期

`ReplyingList` 只有在 session 正常完成并调用 `ClearSessionPending` 后才会释放。上午的“执行”回合持续约 45 分钟且没有正常完成，和 OOM 时间线吻合。

session manager 的 `pendingToolCalls` 也会保留当前工具调用的完整版本，直到 `ClearPendingExchangeAux`。该结构通常只保留每个 CallID 的最新状态，不像 `ReplyingList` 那样逐条累积，但仍需要设置内容上限和异常路径清理保证。

### 5.4 历史会话数据不是首要根因，但会增加分配压力

所有项目的 `.mindfs/sessions` 数据约为：

- JSONL 文件总量：约 406 MiB
- 其中普通 exchange 文件：约 16.6 MiB
- aux 文件：约 389.3 MiB

读取会话详情时，MindFS 会一次读取并解析对应 aux 文件。单个大文件可达十余 MiB，JSON 解码和响应序列化会造成额外瞬时分配，但现有证据不足以说明它单独导致了 1.8 GiB 常驻匿名内存。

## 6. 次要问题与放大因素

### 6.1 Claude Code Router 重启循环

`claude-code-router.service` 原配置为：

```ini
Type=forking
ExecStart=/usr/bin/ccr start
Restart=on-failure
RestartSec=5
```

`ccr start` 实际以前台进程运行，并监听 `127.0.0.1:3456`。systemd 使用 `Type=forking` 时一直等待 fork 完成，约 90 秒后判定启动超时，再等待 5 秒重试。上一轮系统启动累计重试 8,644 次。

CCR 来自独立的第三方 npm 包 `@musistudio/claude-code-router@2.0.0`，既不是官方 Claude Code 自带组件，也不是 MindFS 依赖。MindFS 的 Claude agent 直接配置为 `command=claude`、`protocol=claude-sdk`。

本次已经执行：

```bash
systemctl disable --now claude-code-router.service
```

验证结果：

- service：`disabled`、`inactive`
- `127.0.0.1:3456`：不再监听
- MindFS：继续正常运行

该循环平均 CPU 消耗不高，因此不是本次 OOM 的首要原因，但会持续制造进程、日志和故障噪声。

### 6.2 反向 SSH tunnel 重启循环

`xiaozhen-admin-tunnel.service` 因远端端口 `13003` 已被占用，约每 5 秒失败并重启。重启后的约 25 分钟内已经累计 140 次。该服务同样不是内存耗尽的主要来源，但应修复远端端口冲突或加入合理的启动限制与退避。

### 6.3 cron 重复和锁失效

Stargate 同时存在两个入口：

```text
/var/spool/cron/crontabs/root: 每 5 分钟
/etc/cron.d/sgagenttask:      每 1 分钟
```

命令在 `flock -c` 内部使用了后台执行符 `&`：

```bash
flock -xn /tmp/stargate.lock -c '.../start.sh > /dev/null 2>&1 &'
```

shell 创建后台任务后会立即退出，`flock` 随即释放锁，无法覆盖后台脚本的真实生命周期。

上午 OOM 快照中同时存在：

- 61 个 `cron`
- 22 个 `sh`
- 18 个 `bash`

这些进程单个占用不高，主要是内存/调度已经严重阻塞后形成的任务积压和放大效应。

## 7. 已执行处置

1. 停止并禁用 `claude-code-router.service`。
2. 验证 MindFS 不依赖 CCR，禁用后服务继续正常。
3. 确认 CCR 的 `3456` 端口已经释放。
4. 未删除 CCR npm 包、配置文件或 systemd unit，后续仍可恢复。
5. 本次排查没有修改 MindFS 生产二进制、cron 或 tunnel 配置。

## 8. 建议整改顺序

### P0：修复 MindFS 活跃回合的内存边界

1. 对所有 `commandExecution` 的 `tool_update` 按 `sessionKey + CallID` 合并，使用最新累计输出替换旧事件，而不是持续追加完整副本。
2. 对单个工具输出设置上限，例如 128–256 KiB，仅保留尾部并添加截断标记。
3. 对单个活跃 session 设置总事件数和总字节数上限。
4. 确保成功、失败、取消、超时、客户端断开等所有路径最终都会清理 `ReplyingList` 和 `pendingToolCalls`。
5. 为超长回合增加超时或定期压缩机制。
6. 增加只监听 localhost 的 pprof 或受保护的内存诊断接口，以便下次保存 heap profile。

### P0：控制并发峰值

1. 在 4 GiB 机器上避免同时运行多个前端 `vue-tsc`/构建任务和多个 Agent 长任务。
2. 将构建并发限制为 1。
3. 如果需要长期并行运行 MindFS、Codex、OpenClaw、Docker 和前端构建，建议升级到至少 8 GiB RAM。
4. 不要把“增加 swap”作为根治方案；更多 swap 可能只会延迟 OOM，并让卡死持续更久。

### P1：增加服务资源保护

MindFS unit 目前没有 `MemoryHigh` 或 `MemoryMax`。增加限制前要注意 Codex/OpenClaw 子进程也在同一 cgroup 中，不能直接给 gateway 设置过小的总上限。推荐先评估是否将 Agent worker 拆分为独立 unit/cgroup，再设置：

- gateway 自身的内存上限
- Agent worker 的独立内存上限
- 构建任务的独立 transient scope
- 合理的 `TasksMax`

### P1：清理重启循环和 cron

1. 保持 CCR 禁用；若确实需要，先把 unit 改为 `Type=simple` 再启用。
2. 处理 `xiaozhen-admin-tunnel.service` 的远端端口冲突，并配置启动限速或指数退避。
3. 删除重复 Stargate cron，只保留系统安装方管理的一份。
4. 去掉 `flock -c` 内部的 `&`，让锁覆盖完整脚本执行过程。
5. 为 RSS scheduler 外层再使用内核级 `flock`，避免 JSON 锁在极端压力下失效。

## 9. 后续验证清单

代码修复或资源调整完成后，至少验证：

1. 连续运行长时间、大输出 Codex 命令，`mindfs` RSS 不随累计更新呈持续增长。
2. 回合成功、取消和失败后，内存能够回落。
3. 同时运行一次 `vue-tsc --noEmit` 时，主机仍保留足够可用内存且不会大量换页。
4. `sar -q` 不再出现异常 load average，`sar -u` 的 iowait 保持正常。
5. `journalctl -k` 不再出现 OOM、journald watchdog 或 hung task。
6. `systemctl --failed` 中没有 CCR 或 tunnel 的循环失败。
7. cron 每个计划周期只产生预期数量的任务。

常用检查命令：

```bash
free -h
vmstat 1 5
ps -eo pid,ppid,unit,stat,%cpu,%mem,rss,etimes,comm,args --sort=-rss | head -n 30
systemctl show mindfs.service \
  -p MemoryCurrent -p MemoryPeak -p MemorySwapCurrent -p TasksCurrent
journalctl -k --since today | rg -i 'oom|out of memory|killed process|watchdog|hung task'
sar -q -f /var/log/sysstat/sa$(date +%d)
sar -u ALL -f /var/log/sysstat/sa$(date +%d)
```

## 10. 最终状态

截至本次排查结束：

- MindFS：运行中
- Claude Code Router：已停止并禁用
- 系统负载：恢复正常
- 可用内存：约 2.0 GiB（空闲页与可回收缓存合计）
- swap：仍有约 0.6 GiB 已使用，系统会按需逐步换回，重启前不应仅凭 swap 非零判断仍在泄漏
- MindFS 内存边界修复：尚未实施，仍是最高优先级风险
