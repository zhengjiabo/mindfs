# MindFS Maintenance Agent Notes

## Goal

This repository is no longer maintained as a pure upstream checkout.
It runs as a custom deployment built from `zhengjiabo/main`, while `origin/main`
is treated as upstream.

Any future agent working in this repo should assume:

- upstream remote: `origin` (**fetch only**; no push / no PR by default)
- production remote: `zhengjiabo` (**default push target**)
- production branch: `main` on `zhengjiabo`
- production deployment style: installed binary under `~/.local`, not `./mindfs`
- “push to remote” means `zhengjiabo`, not `origin`, unless the user explicitly says official/upstream


## Git Remotes and Push Policy (MANDATORY)

Default remote for **all** agent push/publish operations is **`zhengjiabo`** only.

| Action | Allowed by default? | Target |
|--------|---------------------|--------|
| `git push` / publish commits | Yes | `zhengjiabo` (`zhengjiabo/main` or current branch on `zhengjiabo`) |
| `git push origin ...` | **No** | — |
| Open PR against `origin/main` / `a9gent/mindfs` | **No** | — |
| Merge into upstream official main | **No** | — |

Rules:

1. When the user says “push / 推送 / 提交到远程 / 合并到远程”, interpret as **`zhengjiabo`**, never `origin`, unless they **explicitly and actively** name official/upstream (`origin`, `a9gent`, 官方, 上游).
2. **Forbidden without explicit user instruction naming official/upstream:**
   - `git push origin ...`
   - `gh pr create --repo a9gent/mindfs ...` or any PR whose base is `origin/main` / upstream main
   - force-push or direct write to `a9gent/mindfs`
3. `origin` is **read-only upstream** for fetch/sync/compare. Pulling or fetching from `origin` is fine; writing is not.
4. Production line remains `zhengjiabo/main`. Do not “helpfully” open upstream PRs after finishing a fix.

If a task seems to require upstream contribution, **stop and ask** instead of pushing to origin or opening an origin PR.

## Production Topology

Current runtime topology:

- local app host: `101.34.62.93`
- local service port: `7331`
- public domain: `https://bc.fireflymoon.cc.cd/`
- public reverse proxy host alias: `hw`
- `hw` nginx upstream target must be `http://101.34.62.93:7331`

Current nginx expectation on `hw`:

- config file: `/etc/nginx/conf.d/bc-subdomain.conf`
- `proxy_pass http://101.34.62.93:7331;`
- after edits: run `nginx -t && systemctl reload nginx`

Do not change the public proxy to `188.239.23.105:7331`.
That IP assumption was incorrect for this environment.


## Installed Runtime

Production must run from the installed binary, not from `./mindfs`.

Installed paths:

- binary: `~/.local/bin/mindfs`
- web assets: `~/.local/share/mindfs/web`
- bundled agent config: `~/.local/share/mindfs/agents.json`
- bundled task templates: `~/.local/share/mindfs/task_template.json`
- logs/pid: `~/.local/share/mindfs`
- config: `~/.config/mindfs`

Useful checks:

```bash
systemctl status mindfs --no-pager
/root/.local/bin/mindfs -status -addr 0.0.0.0:7331 /root/mindfs
ss -ltnp '( sport = :7331 )'
readlink -f /proc/$(pgrep -x mindfs | head -n1)/exe
```

Current production runtime on this server was verified on `2026-07-18` as:

```bash
systemd unit: /etc/systemd/system/mindfs.service
ExecStart=/root/.local/bin/mindfs -foreground -addr 0.0.0.0:7331 /root/mindfs
```

Do not switch production to `./mindfs`, `go run`, Docker, PM2, or a manually managed background process unless the deployment model is intentionally changed.


## E2EE / Pairing

Do not assume E2EE is enabled. Verify `~/.config/mindfs/e2ee.json` first.

Observed state on `2026-07-02`:

- `~/.config/mindfs/e2ee.json` existed
- `"enabled": false`

Important points:

- E2EE config file: `~/.config/mindfs/e2ee.json`
- this file contains `enabled`, `node_id`, and `pairing_secret`
- do not commit the pairing secret into the repository
- if you need the current pairing secret, read it from `~/.config/mindfs/e2ee.json`

If E2EE is enabled and production must keep it enabled, restart with `-e2ee`.
If `enabled` is `false`, do not add `-e2ee` just because old notes mentioned it.

Verification command:

```bash
sed -n '1,120p' /root/.config/mindfs/e2ee.json
```

Do not restart production on `127.0.0.1:7331` if public nginx access is expected.
It must listen on `0.0.0.0:7331`.


## Codex Runtime Notes

Current server-side Codex integration uses the Go SDK app-server transport in:

- `server/internal/agent/codex/session.go`

Important production constraint verified on `2026-07-02`:

- this deployment reads Codex CLI config from `~/.codex/config.toml`
- current provider config on this server points to `https://muyuan.do/v1`
- that channel rejected the old SDK default identity `codex_sdk_go/...` with HTTP `403 Forbidden`
- the SDK used by upstream v0.4.2 defaults to `codex_cli_rs`, but that identity has not been validated against this provider
- the working default identity for this environment is `codex-tui`, not `codex-cli`

The runtime fix in this repo is therefore:

- default `CODEX_INTERNAL_ORIGINATOR_OVERRIDE=codex-tui`
- default SDK `ClientInfo.Name=codex-tui`
- default SDK `ClientInfo.Version` should follow `codex --version`

Optional override env keys supported by MindFS:

- `MINDFS_CODEX_CLIENT_NAME`
- `MINDFS_CODEX_CLIENT_VERSION`

Do not revert those defaults to an SDK-provided identity unless the target provider is known to allow it.
If Codex suddenly starts failing with a channel/client `403`, inspect the detected client string in:

```bash
tail -n 100 /root/.local/share/mindfs/logs/mindfs.log
```

Quick live verification used in this environment:

- `~/.codex/config.toml` currently sets `base_url = "https://muyuan.do/v1"`
- after the client-identity fix and restart, a fresh MindFS Codex request succeeded again against that config


## Git Model

Repository model:

- `origin/main` is official upstream
- `zhengjiabo/main` is the production/custom line
- local `main` should track `zhengjiabo/main`

Check that assumption before doing branch work:

```bash
git branch -vv
git remote -v
```


## Upstream Update Workflow

When upstream changes need to be integrated:

1. fetch both remotes
2. inspect `origin/main` commits before merging
3. create a worktree or integration branch from `origin/main`
4. replay or merge custom patches
5. test
6. update local `main`
7. push to `zhengjiabo/main`

Recommended command pattern:

```bash
git fetch origin --prune
git fetch zhengjiabo --prune
git log --oneline main..origin/main
git worktree add -b integration/$(date +%Y%m%d)-origin-sync ../mindfs-integration origin/main
```

Do not directly `git pull` upstream into the running production directory and restart blindly.


## Build / Release Model

Custom release chain is already wired to `zhengjiabo/mindfs`.

Relevant files:

- `Makefile`
- `scripts/build-all.sh`
- `scripts/install-custom.sh`
- `scripts/generate-release-key.go`
- `.github/workflows/release-custom.yml`
- `.github/workflows/sync-upstream.yml`

Release secrets are expected in GitHub repo `zhengjiabo/mindfs`:

- `MINDFS_RELEASE_PUBLIC_KEY`
- `MINDFS_RELEASE_PRIVATE_KEY`

Local source of release env values:

- `/root/.config/mindfs/release-key.env`

Do not commit the contents of that env file.


## Release Notes Rule

Before creating a release, the first heading in `release-notes.md` must match the tag.

Example:

```md
# MindFS v0.3.4-custom.1
```

Then release with the custom repo values.


## Release Commands

Full custom release:

```bash
set -a
. /root/.config/mindfs/release-key.env
make release \
  TAG=vX.Y.Z-custom.N \
  RELEASE_REPO=zhengjiabo/mindfs \
  RELEASE_REMOTE=zhengjiabo \
  UPDATE_REPO=zhengjiabo/mindfs \
  MINDFS_RELEASE_PUBLIC_KEY="$MINDFS_RELEASE_PUBLIC_KEY" \
  MINDFS_RELEASE_PRIVATE_KEY="$MINDFS_RELEASE_PRIVATE_KEY"
```

If only Linux is needed, limit release platforms:

```bash
set -a
. /root/.config/mindfs/release-key.env
make release \
  TAG=vX.Y.Z-custom.N \
  RELEASE_REPO=zhengjiabo/mindfs \
  RELEASE_REMOTE=zhengjiabo \
  UPDATE_REPO=zhengjiabo/mindfs \
  MINDFS_RELEASE_PLATFORMS=linux/amd64 \
  MINDFS_RELEASE_PUBLIC_KEY="$MINDFS_RELEASE_PUBLIC_KEY" \
  MINDFS_RELEASE_PRIVATE_KEY="$MINDFS_RELEASE_PRIVATE_KEY"
```

Rationale:

- this environment does not require Windows artifacts
- limiting to `linux/amd64` avoids unnecessary `zip` packaging dependency and speeds release creation


## Install / Upgrade Production

Preferred install path is already `~/.local`.

Install from local build:

```bash
set -a
. /root/.config/mindfs/release-key.env
make install \
  PREFIX=/root/.local \
  UPDATE_REPO=zhengjiabo/mindfs \
  MINDFS_RELEASE_PUBLIC_KEY="$MINDFS_RELEASE_PUBLIC_KEY"
```

If a fresh repo update adds frontend dependencies and Vite fails to resolve a package, sync `web/node_modules` before rebuilding:

```bash
cd /root/mindfs/web && npm install
```

Install from custom GitHub release:

```bash
scripts/install-custom.sh --version vX.Y.Z-custom.N --prefix /root/.local
```


## Restart Procedure

When restarting production, use this sequence:

```bash
systemctl restart mindfs
systemctl is-active mindfs
```

Do not use the binary's `-restart` command while systemd owns the process lifecycle.

If E2EE is later re-enabled in `~/.config/mindfs/e2ee.json`, update the unit's `ExecStart` arguments as needed and run `systemctl daemon-reload` before restarting.

Expected healthy state after restart:

```bash
/root/.local/bin/mindfs -status -addr 0.0.0.0:7331 /root/mindfs
curl -fsS http://127.0.0.1:7331/health
curl -kfsS https://bc.fireflymoon.cc.cd/health
```


## Download Authorization Note

With E2EE enabled, direct raw download URLs can fail with `401` if the request does not include proof headers.

Relevant frontend file:

- `web/src/services/download.ts`

The browser download flow should use proof-protected fetch when E2EE is enabled.
If users report `无法下载 - 需要授权`, inspect:

- `web/src/services/download.ts`
- `web/src/services/file.ts`
- `/root/.local/share/mindfs/logs/mindfs.log`

Search pattern:

```bash
grep -n 'raw=1&root=.*download=1 status=' /root/.local/share/mindfs/logs/mindfs.log | tail -n 20
```

If download requests are returning `401`, the browser download path is probably bypassing E2EE proof headers.


## Validation Checklist

After any meaningful change, verify all of the following:

1. installed binary is the one running
2. service listens on `0.0.0.0:7331`
3. local `/health` returns `ok`
4. public `https://bc.fireflymoon.cc.cd/health` returns `ok`
5. update endpoint reports `auto_update_supported: true`
6. nginx on `hw` still proxies to `101.34.62.93:7331`

Recommended commands:

```bash
readlink -f /proc/$(pgrep -x mindfs | head -n1)/exe
ss -ltnp '( sport = :7331 )'
curl -fsS http://127.0.0.1:7331/health
curl -kfsS https://bc.fireflymoon.cc.cd/health
curl -fsS http://127.0.0.1:7331/api/app/update
ssh hw 'grep -n "proxy_pass\|proxy_redirect" /etc/nginx/conf.d/bc-subdomain.conf'
```


## Do Not Forget

- do not expose pairing secrets in repo files
- do not switch production back to `./mindfs`
- do not bind production only to `127.0.0.1` when public nginx access is required
- do not change `hw` proxy target away from `101.34.62.93:7331`
- do not release from `origin/main`; release from the custom production line
- do not `git push origin` or open PRs to `origin/main` / `a9gent/mindfs` unless the user explicitly asks for official/upstream
- default push remote is always `zhengjiabo`
