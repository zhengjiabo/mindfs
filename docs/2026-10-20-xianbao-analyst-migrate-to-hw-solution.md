# xianbao-analyst 迁移到 hw 技术方案

- **文档状态**: 已实施完成(方案 C);2026-10-20 完成收敛清理
- **创建日期**: 2026-10-20
- **迁移对象**: xianbao-analyst(OpenClaw 子 agent 工作区 + PostgreSQL + collector + analyzer)
- **源位置**: 本机 /root/.openclaw/subAgentWorkspace/xianbao-analyst
- **目标主机**: ssh hw(root@188.239.23.105)
- **目标目录**: /opt/xianbao-analyst
- **目标数据卷**: xianbao-analyst_pg_data

> 本方案基于 2026-10-20 的只读勘察。由于本机 collector 每 20 秒写入一次,文档中的行数和 ID 会在实施时继续增长;正式切换前必须重新采集一次基线。

> **2026-10-20 更新**: 最终采用轻量化的方案 C,不再迁移 PostgreSQL 和 OpenClaw。Rust 版工具源码位于 /root/xianbao-lite,包含完整源码、配置示例、systemd 部署文件和 TODO 状态。当前状态:cargo test 11 项通过,release 二进制 863K;真实数据 dry-run 和固定 feed 去重验证通过;已部署到 hw 的 /opt/xianbao-lite;模型 o-main 直接传入;飞书真实测试消息发送成功;notify.dry_run=false,timer enabled + active。
>
> **收敛清理已完成**:OpenClaw xianbao cron 已禁用;本机 xianbao collector / postgres 已停止;数据卷 xianbao-analyst_pg_data(3.3GB)已删除;OpenClaw(龙虾)网关已停止并从 systemd 移除,本地工作目录 /root/.openclaw 与 /root/.openclaw-backups 已删除,仅保留私有 git 备份(github.com/zhengjiabo/openclaw-agents,分支 guagua,commit 9063825)。详细记录见第 12 节。

---

## 0. 进度总览

图例: ✅ 已完成 / 🔄 进行中 / ⬜ 待开始 / ⛔ 阻塞 / ⏸️ 暂缓

| 阶段 | 状态 | 说明 |
| --- | --- | --- |
| 只读勘察 | ✅ 已完成 | 2026-10-20 完成本机与 hw 的只读盘点 |
| 方案文档 | ✅ 已完成 | 本文档,方案 C 为最终选择 |
| 方案 A / B | ❌ 已放弃 | 重量级迁移(PostgreSQL + OpenClaw 上 hw)不再执行 |
| 方案 C 实现 | ✅ 已完成 | Rust 单进程工具 /root/xianbao-lite,tests 11 项通过 |
| 部署到 hw | ✅ 已完成 | /opt/xianbao-lite,binary + config.json + state.json |
| 飞书通知 | ✅ 已完成 | 真实测试消息发送成功,notify.dry_run=false |
| 定时任务 | ✅ 已完成 | systemd timer 每 15 分钟,enabled + active |
| 旧栈退役 | ✅ 已完成 | xianbao cron / collector / postgres / 数据卷 全部清除 |
| OpenClaw(龙虾)退役 | ✅ 已完成 | 网关停用并移除,工作目录删除,仅留私有 git 备份 |
| 24 小时观察 | 🔄 进行中 | timer 自动运行,可随时 journalctl 查看 |

---

## 1. 结论与推荐方案

推荐 **A 方案:数据与应用迁到 hw,OpenClaw 网关和飞书 cron 暂留本机**。

A 方案完成后:

- PostgreSQL、collector、analyzer 全部在 hw 上运行;
- 本机不再运行 xianbao 数据库和采集器,本地数据卷只作为回滚备份;
- OpenClaw 继续负责每 30 分钟的语义分析和飞书通知,只是把执行命令从本机 docker compose 改为 SSH 到 hw 执行;
- 本机不再保存或写入 xianbao 生产数据。

如果要求连 OpenClaw 也搬到 hw、本机完全停用,则采用 B 方案。但 hw 当前只有 1.8GB 内存、约 754MB 可用,存在 OOM 风险,建议先升级到 4GB 内存再执行 B 方案。

---

## 2. 现状盘点

### 2.1 本机现状

| 项目 | 现状 |
| --- | --- |
| 项目目录 | /root/.openclaw/subAgentWorkspace/xianbao-analyst |
| Compose 项目名 | xianbao-analyst |
| PostgreSQL | 16.13 (Debian 16.13-1.pgdg13+1),x86_64 |
| 数据库大小 | 约 3129 MB(3.1GB) |
| 数据卷 | xianbao-analyst_pg_data |
| 数据卷大小 | 约 3.3GB |
| xianbaoku_posts | 1,143,030 行;max(id)=1,143,330;max(created_at)=2026-10-20 16:32:17+08 |
| crawler_runs | 973,482 行;max(id)=973,870;max(started_at)=2026-10-20 16:32:17+08 |
| analysis_status 分布 | filtered=893,450;matched=5,935;pending=239,126;NULL=4,519 |
| 表大小 | xianbaoku_posts=2987 MB;crawler_runs=134 MB |
| 扩展 | 仅 plpgsql |
| 字符集 | UTF8 / en_US.utf8 / en_US.utf8 |
| collector 轮询 | push=20s;rank_1h=320s;request_timeout=10s |
| collector 内存 | 约 24.4 MB |
| postgres 内存 | 约 198.3 MB |
| postgres 镜像 | postgres:16@sha256:2586e2a95d1c9b31cb2967feb562948f7d364854453d703039b6efa45fe48417 |
| collector 镜像 | xianbao-analyst-collector:latest@sha256:a32a06b5615dfe5c5726d1da26acfe975c95fffee753e976f9de7620b89842b1 |
| OpenClaw | 本机运行;gateway 约 416MB RSS;版本 2026.3.24 |
| OpenClaw cron | xianbao-multi-rule-watch,every 30m,enabled=true,lastRunStatus=error |
| cron 最近错误 | HTTP 401: Invalid token |

### 2.2 hw 现状

| 项目 | 现状 |
| --- | --- |
| SSH 别名 | hw(root@188.239.23.105:22) |
| 主机名 | hcss-ecs-ac02 |
| 系统 | Debian GNU/Linux 12(bookworm),kernel 6.1.0-35-amd64,x86_64 |
| CPU / 内存 | 2 核 / 1.8GB,约 754MB available;swap 2GB |
| 磁盘 | / 40GB,已用 23GB,剩余约 16.2GB |
| Docker | 29.2.1,已设置开机启动,当前 active |
| Docker Compose | v5.0.2 |
| Node / npm | Node v22.22.1 / npm 10.9.4 |
| 时区 | Asia/Shanghai(CST) |
| 已有容器 | guoguo-juku-app-1、workbuddy2api、cpa-manager-plus、codex-nexus、gemini-business2api 等 |
| 已有卷 | guoguo-juku_data、guoguo-juku_downloads;无 xianbao 同名冲突 |
| 目标目录 | /opt/xianbao-analyst 当前不存在 |
| postgres:16 镜像 | 未缓存 |
| OpenClaw | 未安装 |
| rsync | 未安装 |
| 网络 | 可访问 new.ixbk.net;飞书网络可达 |
| 端口 | 5432、18789 当前未被占用 |

### 2.3 关键约束

1. OpenClaw cron 当前是 401 错误状态,不修就无法满足“迁移后正常使用”的验收条件。
2. hw 内存余量有限,只迁 xianbao 应用栈可行;连 OpenClaw 一起迁需要先评估或升级内存。
3. hw 没有 rsync,代码和备份文件需要用 scp 或 tar over ssh 传输,或临时安装 rsync。
4. hw 没有缓存 postgres:16 镜像,建议直接搬运本机镜像或明确固定镜像版本/digest。
5. 迁移必须保证数据一致性:正式 dump 前要停 collector,并禁用 OpenClaw cron,避免 dump 后仍有写入。
6. 数据库端口 5432 不应暴露到公网,保持只在 Docker Compose 网络内访问。
7. 本机数据卷在观察期内必须保留,不能直接执行 docker compose down -v 或删除卷。

---

## 3. 目标架构

    +-------------------+        +--------------------------------------+
    |  new.ixbk.net     |        |  hw (188.239.23.105)                 |
    +---------+---------+        |                                      |
              |                  |  +-------------------------------+   |
              |  HTTPS           |  | xianbao-collector             |   |
              +----------------->|  | python scripts/collector/run  |   |
                                 |  +---------------+---------------+   |
                                 |                  |                   |
                                 |                  v                   |
                                 |  +-------------------------------+   |
                                 |  | xianbao-analyst-postgres      |   |
                                 |  | volume: xianbao-analyst_pg_data|  |
                                 |  +---------------+---------------+   |
                                 |                  ^                   |
                                 |                  |                   |
                                 |  +---------------+----------------+  |
                                 |  | analyzer one-shot              |  |
                                 |  | docker compose run --rm        |  |
                                 |  +--------------------------------+  |
                                 +--------------------------------------+
                                                   ^
                                                   | SSH 执行
                                                   |
    +-------------------+        +-----------------+------------------+
    | 飞书               |<-------+ OpenClaw gateway + cron(本机,过渡期) |
    +-------------------+        +------------------------------------+

---

## 4. 方案对比

| 维度 | A 方案:应用/数据迁到 hw,OpenClaw 留本机 | B 方案:全量迁移到 hw |
| --- | --- | --- |
| 迁移范围 | PostgreSQL + collector + analyzer | PostgreSQL + collector + analyzer + OpenClaw |
| 本机是否仍运行 xianbao 数据 | 否 | 否 |
| 本机是否仍需开机 | 是(OpenClaw 网关和飞书通道) | 否 |
| hw 内存风险 | 低(应用栈约 223MB) | 高(OpenClaw 约 416MB + 现有容器约 286MB) |
| 迁移复杂度 | 中 | 高 |
| 对业务影响 | 小,通知链路不变 | 大,需要重建 gateway/cron/凭证 |
| 推荐度 | 推荐 | 内存升级后再考虑 |

---

## 5. TODO 清单

### 5.1 P0 前置

- [x] **P0-1 只读勘察本机与 hw** — 状态:✅ 已完成(2026-10-20)
- [ ] **P0-2 确认采用 A 方案还是 B 方案** — 状态:⬜ 待开始;推荐 A
- [ ] **P0-3 修复 OpenClaw cron 401** — 状态:⬜ 待开始;阻塞验收,需要确认是模型 token 还是飞书 token
- [ ] **P0-4 确认切换窗口与可接受停机时长** — 状态:⬜ 待开始;建议低峰期,预留 30-60 分钟
- [ ] **P0-5 确认是否固定 postgres/collector 镜像版本或 digest** — 状态:⬜ 待开始;建议固定
- [ ] **P0-6 确认 hw 是走应用栈迁移(A)还是同时升级内存(B)** — 状态:⬜ 待开始

### 5.2 预部署

- [ ] **P1-1 在 hw 创建 /opt/xianbao-analyst** — 状态:⬜ 待开始
- [ ] **P1-2 传输项目代码与 .env,排除 .venv 和 __pycache__** — 状态:⬜ 待开始
- [ ] **P1-3 设置 hw 上 .env 权限为 600** — 状态:⬜ 待开始
- [ ] **P1-4 搬运或拉取 postgres:16 与 xianbao-analyst-collector 镜像** — 状态:⬜ 待开始
- [ ] **P1-5 确认 hw 上 Compose 项目名为 xianbao-analyst** — 状态:⬜ 待开始
- [ ] **P1-6 确认 hw 磁盘余量足够容纳 dump + 恢复数据 + 镜像** — 状态:⬜ 待开始;当前约 16.2GB

### 5.3 演练恢复

- [ ] **P2-1 在本机不中断生产的前提下导出演练 dump** — 状态:⬜ 待开始
- [ ] **P2-2 在 hw 用临时数据库恢复演练 dump** — 状态:⬜ 待开始
- [ ] **P2-3 校验表、索引、序列、行数和 max(id)** — 状态:⬜ 待开始
- [ ] **P2-4 校验 analyzer 能从演练库查出候选数据** — 状态:⬜ 待开始
- [ ] **P2-5 记录演练耗时,用于估算正式切换窗口** — 状态:⬜ 待开始

### 5.4 正式切换

- [ ] **P3-1 禁用 OpenClaw cron xianbao-multi-rule-watch** — 状态:⬜ 待开始
- [ ] **P3-2 停止本机 xianbao-collector** — 状态:⬜ 待开始
- [ ] **P3-3 采集切换基线:行数、max(id)、analysis_status 分布** — 状态:⬜ 待开始
- [ ] **P3-4 导出最终冷备份 dump** — 状态:⬜ 待开始
- [ ] **P3-5 传输 dump 到 hw** — 状态:⬜ 待开始
- [ ] **P3-6 在 hw 只启动 postgres** — 状态:⬜ 待开始
- [ ] **P3-7 用 pg_restore 恢复最终 dump** — 状态:⬜ 待开始
- [ ] **P3-8 校验恢复后数据与基线一致** — 状态:⬜ 待开始
- [ ] **P3-9 启动 hw 上的 collector** — 状态:⬜ 待开始
- [ ] **P3-10 确认 crawler_runs 继续增长、日志无异常** — 状态:⬜ 待开始

### 5.5 通知链路切换

- [ ] **P4-1 在 hw 创建 analyzer wrapper 脚本** — 状态:⬜ 待开始
- [ ] **P4-2 把当前 cron 内联规则原样落到 hw wrapper,避免规则漂移** — 状态:⬜ 待开始
- [ ] **P4-3 手动执行 hw wrapper 验证 analyzer 输出** — 状态:⬜ 待开始
- [ ] **P4-4 把 OpenClaw cron 命令改为 SSH 调用 hw wrapper** — 状态:⬜ 待开始
- [ ] **P4-5 手动触发一次 OpenClaw cron,确认飞书收到消息** — 状态:⬜ 待开始
- [ ] **P4-6 确认下一次 30 分钟定时任务成功** — 状态:⬜ 待开始

### 5.6 验收与退役

- [ ] **P5-1 连续 24 小时观察采集、分析、通知** — 状态:⬜ 待开始
- [ ] **P5-2 连续 7 天保留本机数据卷与停机状态** — 状态:⏸️ 暂缓
- [ ] **P5-3 归档本机数据卷** — 状态:⏸️ 暂缓
- [ ] **P5-4 删除本机数据卷并移除本机 collector/cron** — 状态:⏸️ 暂缓
- [ ] **P5-5 在 hw 增加数据库备份与磁盘监控** — 状态:⬜ 待开始

---

## 6. 详细步骤

### 6.1 切换前准备

1. 先修复 OpenClaw cron 的 401,确认手动执行一次可以正常发飞书。
2. 在 hw 创建目录:

       ssh hw 'mkdir -p /opt/xianbao-analyst'

3. 传输代码与 .env,排除本地虚拟环境:

       tar -C /root/.openclaw/subAgentWorkspace \
         --exclude='xianbao-analyst/.venv' \
         --exclude='xianbao-analyst/**/__pycache__' \
         -czf - xianbao-analyst | ssh hw 'tar -xzf - -C /opt'

       ssh hw 'chmod 600 /opt/xianbao-analyst/.env'

4. 搬运镜像,避免 hw 重新 build 受网络影响:

       docker save xianbao-analyst-collector:latest | ssh hw 'docker load'
       docker save postgres:16 | ssh hw 'docker load'

### 6.2 演练恢复

本机导出演练 dump:

    docker exec -T xianbao-analyst-postgres \
      pg_dump -U xianbao -d xianbao -Fc -Z 6 \
      > /root/xianbao-rehearsal.dump

传到 hw:

    scp /root/xianbao-rehearsal.dump hw:/opt/xianbao-analyst/

在 hw 用临时数据库恢复并校验。重点检查:

- 表、索引、序列都恢复成功;
- xianbaoku_posts 和 crawler_runs 行数一致;
- max(id) 一致;
- analyzer 能查询到候选数据;
- 记录整个演练耗时。

### 6.3 正式切换

1. 禁用 OpenClaw cron:

       openclaw cron disable <xianbao-multi-rule-watch 的任务 ID>

2. 停止本机 collector:

       cd /root/.openclaw/subAgentWorkspace/xianbao-analyst
       docker compose -p xianbao-analyst stop collector

3. 采集切换基线:

       docker exec xianbao-analyst-postgres psql -U xianbao -d xianbao -Atc "
       select count(*), max(id) from xianbaoku_posts;
       select count(*), max(id) from crawler_runs;
       select analysis_status, count(*) from xianbaoku_posts group by 1 order by 1;
       "

4. 导出最终冷备份:

       docker exec -T xianbao-analyst-postgres \
         pg_dump -U xianbao -d xianbao -Fc -Z 6 \
         > /root/xianbao-final.dump

5. 传到 hw:

       scp /root/xianbao-final.dump hw:/opt/xianbao-analyst/

### 6.4 hw 恢复与启动

1. 只启动 PostgreSQL,先不要启动 collector:

       ssh hw 'cd /opt/xianbao-analyst && docker compose -p xianbao-analyst up -d postgres'

2. 把 dump 放入容器并恢复:

       ssh hw 'docker cp /opt/xianbao-analyst/xianbao-final.dump xianbao-analyst-postgres:/tmp/xianbao-final.dump'

       ssh hw 'docker exec xianbao-analyst-postgres pg_restore -U xianbao -d xianbao --no-owner --no-privileges -j 2 /tmp/xianbao-final.dump'

3. 恢复完成后启动 collector:

       ssh hw 'cd /opt/xianbao-analyst && docker compose -p xianbao-analyst up -d collector'

4. 检查:

       ssh hw 'cd /opt/xianbao-analyst && docker compose ps'
       ssh hw 'docker logs --tail=80 xianbao-collector'

### 6.5 cron 切到 hw

建议在 hw 上创建固定入口脚本 /opt/xianbao-analyst/bin/analyze.sh,内部执行:

    cd /opt/xianbao-analyst
    docker compose -p xianbao-analyst run --rm collector \
      python scripts/analyzer/run_once.py "$(cat /opt/xianbao-analyst/run_config.json)"

然后把 OpenClaw cron 消息里的本机命令改为:

    ssh hw '/opt/xianbao-analyst/bin/analyze.sh'

注意:当前 cron 里的规则是内联 JSON,和 run_config.json 可能有细微差异。迁移时应把当前 cron 的 payload 原样落到 hw wrapper,避免规则漂移。

### 6.6 本机退役

1. 不要马上删除数据卷。先停止本机应用栈:

       cd /root/.openclaw/subAgentWorkspace/xianbao-analyst
       docker compose -p xianbao-analyst down

2. 观察 7 天。期间如 hw 出问题,可直接重新启用本机 cron、启动本机 collector 回滚。
3. 7 天后归档本机数据卷,再删除卷和本机 collector/cron。
4. 严禁使用 docker compose down -v 直接删除卷。

---

## 7. 验收标准

| 检查项 | 通过标准 | 当前状态 |
| --- | --- | --- |
| hw 容器状态 | postgres 与 collector 均为 Up | ⬜ 待开始 |
| 数据连续性 | xianbaoku_posts 与 crawler_runs 行数和 max(id) 与切换基线一致 | ⬜ 待开始 |
| 采集恢复 | 启动后 1 分钟内出现新的 crawler_runs 记录 | ⬜ 待开始 |
| analyzer | 能输出 JSON,并正常写回 analysis_status | ⬜ 待开始 |
| OpenClaw cron | 手动触发一次,飞书收到测试消息 | ⬜ 待开始 |
| 定时任务 | 下一次 30 分钟任务成功,无 401 | ⬜ 待开始 |
| 稳定性 | 连续 24 小时无 collector 异常、无数据库连接错误 | ⬜ 待开始 |
| 本机停止写入 | 本机 collector 已停止,本地卷无新写入 | ⬜ 待开始 |

---

## 8. 回滚方案

观察期内保留本机完整数据卷,回滚路径如下:

1. 如果 hw 启动失败或数据异常,且 hw 尚无新数据:
   - 重新启用本机 OpenClaw cron;
   - 启动本机 collector:

         cd /root/.openclaw/subAgentWorkspace/xianbao-analyst
         docker compose -p xianbao-analyst start collector

2. 如果 hw 已经产生新数据:
   - 先从 hw 导出一份最新 dump;
   - 再决定是把 hw 数据导回本机,还是接受这段数据缺口。

3. 回滚期间不要删除 hw 上的卷和本机卷,两边都保留,直到确认最终方向。

---

## 9. 风险与待确认

| 风险 / 待确认项 | 影响 | 建议 |
| --- | --- | --- |
| OpenClaw cron 当前 401 | 迁移后采集正常但通知不正常 | 切换前修复,确认是模型 token 还是飞书 token |
| hw 内存仅 1.8GB | 只迁应用栈风险低;连 OpenClaw 一起迁 OOM 风险高 | 采用 A 方案;B 方案先升级到 4GB |
| 数据卷 3.3GB + dump + 镜像 | hw 磁盘消耗约 5-6GB | 当前剩余 16.2GB,充足;恢复后删除 dump |
| 镜像版本漂移 | postgres:16 / python:3.11-slim 浮动 tag 可能拉到新版本 | 固定 digest 或直接搬运本机镜像 |
| 规则漂移 | cron 内联规则与 run_config.json 不一致 | 迁移时原样迁移 cron payload |
| 切换期间写入丢失 | dump 后本机仍有写入 | 先禁 cron、再停 collector、最后 dump |
| 数据库长期增长 | 目前约 3.1GB,持续增长 | 增加备份、磁盘监控和旧数据清理策略 |
| 5432 暴露 | 公网安全风险 | 保持不暴露,只在 Compose 网络内访问 |

---

## 10. 进度日志

| 日期 | 事项 | 状态 | 说明 |
| --- | --- | --- | --- |
| 2026-10-20 | 只读勘察本机 xianbao-analyst | ✅ 已完成 | PostgreSQL 16.13,数据卷 3.3GB,collector 运行中 |
| 2026-10-20 | 只读勘察 hw | ✅ 已完成 | Debian 12,2C/1.8GB,16.2GB 可用,Docker 已启用 |
| 2026-10-20 | 输出迁移技术方案 | ✅ 已完成 | 本文档 |
| 2026-10-20 | 改用方案 C(Rust 单进程) | ✅ 已完成 | 放弃迁移 PostgreSQL / OpenClaw |
| 2026-10-20 | 实现 /root/xianbao-lite | ✅ 已完成 | cargo test 11 项通过,release 863K |
| 2026-10-20 | 部署 hw + 飞书真实推送 | ✅ 已完成 | /opt/xianbao-lite,timer 每 15 分钟 |
| 2026-10-20 | 清理本机旧 xianbao 栈 | ✅ 已完成 | cron / collector / postgres / 3.3GB 数据卷已删 |
| 2026-10-20 | 停止并移除 OpenClaw 网关 | ✅ 已完成 | 服务 disabled 且单元文件移除,端口 18789 关闭 |
| 2026-10-20 | OpenClaw 全量备份到 git | ✅ 已完成 | zhengjiabo/openclaw-agents@guagua = 9063825(私有仓库) |
| 2026-10-20 | 删除 OpenClaw 本地目录 | ✅ 已完成 | /root/.openclaw、/root/.openclaw-backups、/tmp/openclaw 已删 |

---

## 12. 收敛清理记录(2026-10-20)

### 12.1 本机执行结果

| 项目 | 处理 | 结果 |
| --- | --- | --- |
| xianbao-analyst_pg_data 数据卷 | 删除 | 释放约 3.3GB |
| xianbao-analyst-collector / postgres:16 镜像 | 删除 | 本机不再有 xianbao 数据库 |
| 本机 xianbao 老工作区 / 老 agent | 删除 | 随 OpenClaw 仓库清理一并移除 |
| openclaw-gateway(龙虾) | 停止 + 从 systemd 移除 | 端口 18789 / 18791 已关闭,释放约 415MB RSS |
| /root/.openclaw | 删除 | 约 812MB |
| /root/.openclaw-backups | 删除 | 约 228MB |
| /tmp/openclaw、/tmp/openclaw-0、/var/log/openclaw.log | 删除 | 约 20MB |
| /usr/lib/node_modules/openclaw、/usr/bin/openclaw | **保留** | 是当前 agent 运行环境(mindfs 的 ACP 提供方)依赖,不是龙虾工作区 |

本机磁盘:清理前剩余 9.1GB → 清理后剩余 11GB(根分区 40GB)。内存已用:1.35GB → 启动网关时约 1.6GB。

### 12.2 git 备份状态(唯一保留的龙虾痕迹)

| 项 | 值 |
| --- | --- |
| 仓库 | github.com/zhengjiabo/openclaw-agents(PRIVATE) |
| 分支 | guagua |
| 最新 commit | 9063825(backup: add gateway systemd unit and restore notes) |
| 远端校验 | git ls-remote origin guagua 与本地 HEAD 一致,工作区 clean |
| 备份内容 | openclaw.json 及历史备份、agents/、cron/、extensions/、quarantine/、deploy/openclaw-gateway.service、deploy/RESTORE.md |
| 刻意未备份 | sessions/、cron/runs/、logs/、identity/device-auth.json、memory/*.sqlite、*/data/*.json、node_modules(见仓库 .gitignore 设计) |

> 恢复步骤写在仓库内 deploy/RESTORE.md。用户已明确本次不需要恢复,该仓库仅作后续保险。

### 12.3 hw 现状

- /opt/xianbao-lite 运行正常;systemd timer enabled + active,每 15 分钟执行一次,最近一次 Result=success、退出码 0(当次 pushed=0 属去重正常)。
- hw 上不存在 openclaw 或 xianbao-analyst 残留,也没有为本次迁移新建的数据库容器。
- 查看日志:ssh hw 'journalctl -u xianbao-lite.service -n 50 --no-pager'。

## 11. 附录:关键路径与命令速查

| 用途 | 命令 / 路径 |
| --- | --- |
| 本机项目目录 | /root/.openclaw/subAgentWorkspace/xianbao-analyst |
| hw 目标目录 | /opt/xianbao-analyst |
| Compose 项目名 | xianbao-analyst |
| 数据卷名 | xianbao-analyst_pg_data |
| 本机停止 collector | docker compose -p xianbao-analyst stop collector |
| hw 启动 | cd /opt/xianbao-analyst && docker compose -p xianbao-analyst up -d |
| hw 查看状态 | cd /opt/xianbao-analyst && docker compose ps |
| hw 查看日志 | docker logs --tail=100 xianbao-collector |
| hw 检查数据 | docker exec xianbao-analyst-postgres psql -U xianbao -d xianbao -Atc "select count(*) from xianbaoku_posts" |
