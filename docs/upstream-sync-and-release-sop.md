# MindFS 官方同步与自定义发布 SOP

## 1. 目的

本文档用于约束 MindFS 在以下场景下的长期维护方式：

- 需要持续跟进官方 `a9gent/mindfs` 更新。
- 同时保留本地自定义需求。
- 希望升级过程稳定、可回滚、尽量少影响已部署实例。
- 希望最终形成接近“无痛升级”的操作路径。

本文档的核心结论只有一句话：

**生产环境不再直接运行仓库里的源码构建产物，而是运行安装版布局的发布二进制；官方更新先并入自定义分支，再由自定义分支产出自己的 release。**


## 2. 目标架构

### 2.1 分支职责

- `origin/main`
  - 官方上游分支。
  - 只做同步，不直接承载本地定制。

- `zhengjiabo/main`
  - 自定义生产分支。
  - 承载所有需要长期保留的本地补丁。
  - 生产发布必须从这个分支产出。

- 临时集成分支
  - 用于每次跟进官方更新时的合并、测试、冲突处理。
  - 命名建议：`integration/YYYYMMDD-origin-sync`

### 2.2 运行形态

生产环境应使用安装版布局，而不是直接运行仓库根目录下的 `./mindfs`。

推荐布局：

```text
PREFIX/
  bin/
    mindfs
  share/
    mindfs/
      agents.json
      task_template.json
      web/
```

Linux/macOS 默认推荐：

```text
~/.local/bin/mindfs
~/.local/share/mindfs/
```

### 2.3 数据目录

用户数据与安装目录分离，正常升级时不应删除：

- 配置目录：`~/.config/mindfs`
- 状态和日志目录：`~/.local/share/mindfs`

其中通常包括：

- `registry.json`
- `agents.json`
- `preferences.json`
- `e2ee.json`
- `credentials.json`
- `prompts.json`
- 日志文件和 pid 文件


## 3. 为什么不能直接长期跑官方源码

不建议长期使用“仓库目录 + `make build` + `./mindfs`”作为生产部署方式，原因如下：

1. 官方自升级要求安装版布局。
2. 源码目录容易混入本地调试改动、未提交文件、冲突解决残留。
3. 运行目录和开发目录混在一起，升级和回滚都不干净。
4. 一旦保留自定义代码，直接跟官方 release 会覆盖本地定制。

结论：

**源码目录用于开发和集成，安装版目录用于部署和升级。**


## 4. 长期策略

### 4.1 总原则

每次官方更新，必须先经过以下链路：

```text
origin/main
  -> 集成分支
  -> 合入/重放本地补丁
  -> 验证
  -> 更新 zhengjiabo/main
  -> 产出自定义 release
  -> 部署到安装版目录
```

### 4.2 自定义需求分类

为了降低跟官方时的冲突成本，自定义需求分成两类。

第一类：尽量外置，不改核心代码。

- agent 配置
- shell 配置
- 启动参数
- TLS / 反向代理 / 端口
- 部署脚本
- systemd / supervisor / pm2 等守护配置

第二类：确实必须改代码。

这类改动必须满足：

- 一个需求一个 commit
- commit message 明确说明目的
- 尽量避免把多个需求混在同一个 commit
- 能上游贡献的优先尝试提给官方


## 5. 当前约束与现实判断

当前仓库状态已经说明以下事实：

1. 本地正在运行的是仓库根目录下的 `./mindfs`，不是安装版路径。
2. 当前本地已有自定义补丁，且已经推送到 `zhengjiabo/main`。
3. 官方更新会改动高频文件，例如 `server/app/server.go`，与本地补丁存在冲突风险。

因此，**当前阶段不应该直接在生产目录执行 `git pull` 然后覆盖运行。**


## 6. 标准工作流

### 6.1 平时开发工作流

1. `origin/main` 保持纯净。
2. 自定义补丁进入 `zhengjiabo/main`。
3. 每次集成官方更新时，使用临时分支或 `git worktree`，不要直接在生产运行目录上做。

建议命令：

```bash
git fetch origin --prune
git fetch zhengjiabo --prune
git switch main
git pull --ff-only zhengjiabo main
git branch --set-upstream-to=zhengjiabo/main main
git worktree add -b integration/$(date +%Y%m%d)-origin-sync ../mindfs-integration origin/main
```

说明：

- 仓库当前 `main` 可以视为自定义主线，和 `zhengjiabo/main` 保持一致。
- 本地 `main` 应显式跟踪 `zhengjiabo/main`，避免默认拉到 `origin/main`。
- 集成时基于 `origin/main` 新建临时工作区最稳妥。

### 6.2 官方更新同步工作流

每次准备跟进官方更新时，按下面步骤执行。

#### 步骤 1：同步远端信息

```bash
git fetch origin --prune
git fetch zhengjiabo --prune
git log --oneline main..origin/main
```

先看清楚官方新增了哪些提交，是否涉及以下高风险区域：

- `server/app/`
- `server/internal/update/`
- `server/internal/agent/`
- `cli/cmd/mindfs.go`
- `web/src/App.tsx`
- `web/src/components/`

#### 步骤 2：创建集成分支

```bash
git worktree add -b integration/20260613-origin-sync ../mindfs-integration origin/main
cd ../mindfs-integration
```

#### 步骤 3：重放自定义补丁

如果自定义补丁数量少，优先使用 `cherry-pick`：

```bash
git cherry-pick <custom-commit-1>
git cherry-pick <custom-commit-2>
```

如果补丁已经形成稳定补丁队列，也可以考虑按顺序 `cherry-pick` 一组固定 commit。

#### 步骤 4：解决冲突

冲突处理原则：

1. 先理解官方改动的目标。
2. 再判断本地需求是否仍然成立。
3. 避免为了保留本地补丁而覆盖官方的新行为。
4. 冲突处理完成后，重新梳理代码，确保不是把两边逻辑简单拼接在一起。

#### 步骤 5：验证

至少执行以下检查：

```bash
make build
./mindfs --version
```

如果涉及 Go 侧核心逻辑，增加：

```bash
go test ./server/...
```

如果涉及 Web 侧关键流程，增加：

```bash
cd web
npm install
npm run build
cd ..
```

如果当前改动触及会话、agent、update、project discovery 等共享逻辑，应扩大验证范围。

#### 步骤 6：回写到自定义主分支

验证通过后，将集成结果更新到 `zhengjiabo/main`。

通常有两种方式：

- 方式 A：让集成分支 merge 到本地 `main`
- 方式 B：本地 `main` rebase / fast-forward 到集成结果

推荐保持历史清晰即可，不强求某一种，但必须保证 `zhengjiabo/main` 始终代表可发布状态。

最后推送：

```bash
git push zhengjiabo main
```


## 7. 发布策略

### 7.1 推荐发布方式

不建议直接把编译好的仓库产物复制到线上长期使用。

推荐方式：

1. 从 `zhengjiabo/main` 产出正式二进制包。
2. 包结构对齐官方 release 布局。
3. 将包发布到自己的 GitHub Releases。

目标是让生产环境安装的是“自己的安装版”，而不是“开发目录里的临时构建物”。

### 7.2 安装版目录要求

必须保证部署后二进制路径类似：

```text
PREFIX/bin/mindfs
```

并同时存在：

```text
PREFIX/share/mindfs/agents.json
PREFIX/share/mindfs/task_template.json
PREFIX/share/mindfs/web/
```

这样后续更新逻辑才有机会稳定工作。

### 7.3 更新源策略

如果未来希望在应用内或脚本级“一键升级”到自定义版本，必须补充一项基础设施：

**将更新源从写死的官方仓库改为可配置。**

推荐实现方式之一：

- 增加环境变量，例如：`MINDFS_UPDATE_REPO`
- 默认值仍为 `a9gent/mindfs`
- 自定义发布环境中改为自己的 release 仓库

在没有完成这一改造之前：

- 官方更新检查仍然会默认看 `a9gent/mindfs`
- 直接使用内置更新能力，会把自定义版本覆盖成官方版本

因此，**在更新源可配置之前，不允许在自定义生产环境中直接使用官方自升级入口。**


## 8. 部署 SOP

### 8.1 首次从源码部署迁移到安装版部署

目标：从“运行仓库里的 `./mindfs`”迁移为“运行安装目录里的 `mindfs`”。

#### 步骤 1：备份配置

```bash
cp -a ~/.config/mindfs ~/.config/mindfs.backup.$(date +%Y%m%d%H%M%S)
```

必要时也可备份状态目录：

```bash
cp -a ~/.local/share/mindfs ~/.local/share/mindfs.backup.$(date +%Y%m%d%H%M%S)
```

#### 步骤 2：准备安装版目录

推荐前缀：

```text
~/.local
```

如果是自定义发布版，也可以使用：

```text
~/apps/mindfs
```

但必须保持 `bin/` 和 `share/mindfs/` 的结构。

#### 步骤 3：停止当前旧实例

如果当前运行的是仓库内的二进制：

```bash
cd /path/to/repo
./mindfs --stop
```

#### 步骤 4：安装新版本

如果是官方安装版：

```bash
curl -fsSL https://raw.githubusercontent.com/a9gent/mindfs/main/scripts/install.sh | bash
```

如果是自定义 release，应使用自定义 release 包进行安装，保持同样目录结构。

#### 步骤 5：确认路径

```bash
which mindfs
mindfs --version
```

目标是看到安装目录下的 `mindfs`，而不是仓库里的 `./mindfs`。

#### 步骤 6：启动新实例

```bash
mindfs
```

或带参数启动：

```bash
mindfs /path/to/project
```

#### 步骤 7：验证

至少检查：

- 服务能正常启动
- 浏览器可访问
- 现有项目列表存在
- 现有 agent 配置仍在
- 历史会话能打开
- 日志无明显异常


## 9. 升级 SOP

### 9.1 自定义生产环境升级步骤

生产环境每次升级，按以下流程执行：

1. 在代码仓库里完成官方同步和自定义补丁集成。
2. 从 `zhengjiabo/main` 产出新 release。
3. 在部署机上备份配置目录。
4. 停止当前进程。
5. 用新 release 覆盖安装目录。
6. 启动新版本。
7. 验证核心功能。
8. 观察日志。

### 9.2 升级前检查清单

升级前必须确认：

- 当前 `zhengjiabo/main` 已推送
- 当前要升级的 commit 已完成验证
- 备份已完成
- 变更说明已阅读
- 若涉及数据结构或配置格式变化，已明确兼容性风险


## 10. 回滚 SOP

### 10.1 何时回滚

满足任一条件应优先回滚，而不是在线上继续修：

- 服务无法启动
- 关键页面空白或报错
- agent 无法连接
- 项目列表异常消失
- 历史会话不可读
- 日志持续刷严重错误

### 10.2 回滚步骤

1. 停止当前新版本。
2. 切回上一个安装包或上一个已知可用二进制。
3. 启动旧版本。
4. 如确认配置损坏，再恢复 `~/.config/mindfs` 备份。

说明：

- 回滚优先级是“先切回旧二进制”，不要上来就恢复配置目录。
- 只有在明确判断配置写坏、格式不兼容时，才恢复配置备份。


## 11. 补丁维护规则

### 11.1 每个补丁必须独立提交

错误示例：

- 一个 commit 同时包含项目发现修复、agent 配置调整、前端文案修改

正确示例：

- `fix: skip unsafe auto project roots`
- `feat: support custom update repo`
- `refactor: isolate hosted agent config merge`

### 11.2 补丁要尽量薄

目标不是在自定义分支上大规模重写官方逻辑，而是把差异控制在最小范围。

判断标准：

- 能通过配置解决的，不通过代码解决。
- 能通过新增小函数隔离的，不直接在大函数里铺开改动。
- 能上游化的，优先尝试上游化。

### 11.3 对高频文件保持警惕

以下文件或目录属于高频变动区，补丁落在这些位置时要预估后续冲突成本：

- `server/app/server.go`
- `cli/cmd/mindfs.go`
- `server/internal/update/`
- `server/internal/agent/`
- `web/src/App.tsx`
- `web/src/components/*`


## 12. 后续必须补齐的基础设施

为了真正达到“官方一更新，可以低成本跟进”的目标，后续需要补齐以下能力。

### 12.1 更新源可配置

必须支持将更新检查与下载安装的仓库从官方默认值切换为自定义仓库。

建议：

- 环境变量：`MINDFS_UPDATE_REPO`
- 默认值：`a9gent/mindfs`

当前仓库已经落地第一版运行时支持：

- 服务启动后，更新检查会优先读取 `MINDFS_UPDATE_REPO`
- 未设置时仍回退到 `a9gent/mindfs`
- 自定义构建默认会把更新源编译为 `zhengjiabo/mindfs`

生产环境建议设置为：

```bash
export MINDFS_UPDATE_REPO="zhengjiabo/mindfs"
```

### 12.2 自定义安装脚本或发布脚本

需要一份面向自定义 release 的安装脚本，至少完成：

- 安装 `bin/mindfs`
- 安装 `share/mindfs/web`
- 安装 `share/mindfs/agents.json`
- 安装 `share/mindfs/task_template.json`
- 保留用户配置目录
- 支持覆盖升级

当前仓库已增加：

- 自定义安装脚本：`scripts/install-custom.sh`
- 自定义 release 工作流：`.github/workflows/release-custom.yml`

### 12.3 自动化集成流程

建议逐步增加自动化：

- 自动同步 `origin/main`
- 自动重放固定补丁队列
- 自动构建 release 包
- 自动发布到自定义 GitHub Releases

有了这三项，后续升级才会真正接近“无痛”。


## 13. 当前执行建议

按照本 SOP，当前应按以下顺序推进：

1. 先保留现有 `zhengjiabo/main` 作为自定义生产主线。
2. 不在当前运行目录直接跟官方。
3. 每次官方更新时，在集成工作区完成合并和测试。
4. 补齐“更新源可配置”能力。
5. 将部署形态迁移为安装版目录结构。
6. 再考虑把自定义 release 做成标准安装包。


## 14. 简版操作清单

### 日常同步官方

```bash
git fetch origin --prune
git fetch zhengjiabo --prune
git log --oneline main..origin/main
git worktree add -b integration/$(date +%Y%m%d)-origin-sync ../mindfs-integration origin/main
cd ../mindfs-integration
git cherry-pick <custom-commits>
make build
go test ./server/...
```

### 更新自定义远端

```bash
git switch main
git merge --ff-only integration/<date>-origin-sync
git push zhengjiabo main
```

### 迁移到安装版部署

```bash
cp -a ~/.config/mindfs ~/.config/mindfs.backup.$(date +%Y%m%d%H%M%S)
cd /path/to/repo
./mindfs --stop
curl -fsSL https://raw.githubusercontent.com/a9gent/mindfs/main/scripts/install.sh | bash
which mindfs
mindfs --version
mindfs
```


## 15. 禁止事项

以下操作默认禁止：

- 在生产运行目录直接执行 `git pull` 后覆盖运行
- 在未备份配置目录前直接升级生产环境
- 在自定义生产版本上直接使用官方自升级入口
- 将多个无关自定义需求混在一个 commit
- 在未验证的情况下把集成结果直接推到 `zhengjiabo/main`
