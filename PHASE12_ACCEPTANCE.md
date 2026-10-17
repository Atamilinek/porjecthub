# Phase 12 — Formal User Acceptance and Daily Use Handover

> **公开版本说明**：本文件中的实机标识已替换为占位符（`<LAN_IP>`、`<LAN_GATEWAY>`、`<WIFI_MAC>`、`<HOSTNAME>`、`<NVME_MODEL>`、`<LOCAL_CA_SHA256>`）。验收日期、测量结果、命令与通过/未通过结论保持不变；本地复核时请替换为你自己的值。

验收日期：2026-10-12（Asia/Shanghai）

设备：Orange Pi Zero 3W，Debian 13 ARM64，`<HOSTNAME>`

服务地址：`https://<LAN_IP>/`

数据盘：`<NVME_MODEL>`，`/dev/nvme0n1p1` → `/srv/data`

本报告把已运行的只读检查、隔离恢复测试和需要设备所有者完成的验收分开记录。生产数据库、路由器配置、宿主机服务和启动状态未被破坏性操作触碰。

## 验收状态

| 项目 | 结果 | 实测与边界 |
|---|---|---|
| HTTPS、API、Agent、Docker 和 NVMe | **只读复核通过** | 2026-10-12 00:02 CST：`https://<LAN_IP>/healthz` 返回 HTTP 200，使用当前 CA 验证结果 0；未认证 `/api/v1/projects` 返回 HTTP 401。Docker、`projecthub.service`、`projecthub-agent.service` 均 active；Compose 项目 `projecthub` 的 API healthy、Caddy running。NVMe SMART passed，58°C，critical warning 0、media errors 0；Root Port Link Up，8.0 GT/s ×1。`/srv/data` 是 ext4，挂载于 NVMe 分区；Docker root 为 `/srv/data/docker`。没有重启或服务操作。 |
| 生产 SQLite | **只读完整性通过** | 当前数据库位于 `projecthub_projecthub-data`，路径 `/srv/data/docker/volumes/projecthub_projecthub-data/_data/projecthub.db`，权限 0600。SQLite `PRAGMA integrity_check` 返回 `ok`。只读统计：1 项目、0 分类、2 用户、1 主机、4 服务目标、4 检查。没有查询或导出密码哈希。 |
| TLS 设备端到端验收 | **Pi curl 通过；浏览器/手机待人工验收** | 当前 CA 下健康检查证书校验通过，Phase 11 已验证 leaf SAN 为 IP `<LAN_IP>`。本阶段没有安装 Windows/Android/iOS CA，没有跳过浏览器证书验证。依设备步骤见下方清单。 |
| 路由器 DHCP 地址保留 | **待人工操作** | 当前 `wlan0` 地址 `<LAN_IP>/24`，MAC `<WIFI_MAC>`，默认网关 `<LAN_GATEWAY>`。未登录路由器、未修改配置，也未验证 reservation。 |
| 管理员密码和会话失效 | **实现与测试已核对；生产密码未轮换** | 管理中心允许管理员重置账户密码；服务层要求 12–1024 字节密码，并在事务中删除该用户全部会话。会话有效期 12 小时。`go test ./internal/auth ./cmd/api` 通过；用户管理测试确认重置后旧 session token 得到 `ErrInvalidSession`。当前 bootstrap 密码文件仍存在，权限 0600；没有读取其内容，也没有用真实管理员凭据登录。管理员自重置后应刷新页面并使用新密码重新登录。 |
| 数据库备份与异机恢复 | **新鲜备份和隔离恢复通过；异机副本未建立** | 新的在线快照 `/srv/data/projecthub/backups/phase12-acceptance-20261011T160412Z.db`，模式 0600，SHA-256 `fa5fb355158e57b8111685acd1f223922fa53c59db33705f41850578157761f5`；同目录 `.sha256` sidecar 模式 0600，`sha256sum -c` 返回 OK。将该备份只读挂载进 `--network none` 的一次性 API 容器，数据库路径位于 64 MiB 临时 tmpfs，`projecthub-api restore` 返回成功并执行迁移及 `integrity_check`。生产数据库卷没有挂载到恢复容器，生产数据未被恢复覆盖。当前备份仍在同一块 NVMe；还没有配置异机传输、异地保留或接收端加密。 |
| 验证 Compose 资源 | **清点完成；未删除** | 验证项目 `phvalidation20261011`：API 容器 exited 127，Caddy 仍运行并只绑定 `127.0.0.1:39081` 与 `127.0.0.1:39443`。API 的只读 Agent-socket bind 源 `/run/projecthub-agent-validation-20261011/agent.sock` 当前实际是目录，不是 socket；Docker 记录的启动错误为“mount a directory onto a file (or vice-versa)”。Caddy 使用专用 Caddyfile 和两个专用 Caddy 卷。验证网络与生产不同；`probes` 当前无活动端点。三个验证卷均与生产卷分离。另有一个 Compose 项目外的停止测试目标，及含验证 DB/credential/policy 的本地目录；清理方案见下方。没有执行 down、stop、prune 或删除。 |
| 项目与分类真实录入 | **功能已实现；生产数据验收待管理员完成** | 当前库有 1 项目但没有分类。本阶段未代替所有者创造或修改真实项目数据。录入与复核流程见下方清单。 |
| NVMe 持续监控及冷启动 | **当前快照通过；长期稳定性待测** | 已安装的 `/usr/local/bin/projecthub-nvme-health-check` 在本阶段再运行一次，报告 Link Up、8 GT/s ×1，SMART passed，介质错误 0。健康日志 `num_err_log_entries=1` 与单独 `smartctl -l error` 的 “No Errors Logged” 均保留观察；不能将其等同于介质错误。脚本目前手动执行，没有新增 timer/cron/journald 配置。历史 PCIe 训练失败仍未通过连续冷启动验证；本阶段未重启。 |
| Node 与前端构建版本 | **固定并通过** | 新增根 `.nvmrc` 固定 Node `24.21.0`；`web/package.json` 声明同一精确引擎版本及 npm `11.19.0`；CI 从 `.nvmrc` 读取；Dockerfile 已使用 `node:24.21.0-alpine`。从 Node 官方发行包校验 SHA-256 后，在 Node `v24.21.0` / npm `11.19.0` Linux 临时副本完成 `npm ci`（无 EBADENGINE 警告）、Vitest 6/6、typecheck 和 production build。 |

生产 API 当前记录的 4 个 health check 的 `current_state` 全部为 `healthy`。Agent policy 文件权限为 root:root 0600，列出 4 个精确 systemd unit 目标且仅允许 `status`；Agent socket 为 root:`projecthub-api` 0660。生产 API 只读挂载 Agent socket，不挂载 Docker Socket；Docker 数据根目录 `/srv/data/docker`。systemd 当前仍满足：Docker `Requires=srv-data.mount` 且 `RequiresMountsFor=/srv/data`；Project Hub `Requires=docker.service,srv-data.mount` 且 `RequiresMountsFor=/srv/data/projecthub`。

### 遗留验证 Compose 资源明细

| 资源 | 镜像/挂载/网络/端口 | 关系与状态 |
|---|---|---|
| `phvalidation20261011-api-1` | 镜像 `projecthub-api:validation-arm64-20261011-final`；无宿主机发布端口；专用数据卷挂载到 `/var/lib/projecthub`；验证备份目录挂载到 `/backups`；只读 bind `/run/projecthub-agent-validation-20261011/agent.sock`；配置网络 `private` 和 `probes`；restart `unless-stopped` | 已退出 127。Agent socket 源文件缺失；不会触及生产 Agent socket。 |
| `phvalidation20261011-caddy-1` | 镜像 `caddy:2.10-alpine`；验证 Caddyfile 只读挂载；专用 Caddy data/config 卷；网络 `private` 与 `edge`；`127.0.0.1:39081→80/tcp`、`127.0.0.1:39443→443/tcp,udp`；restart `unless-stopped` | 仍 running，但只可从 Orange Pi 本机回环端口访问。API unhealthy/退出时，Caddy 的 `depends_on: api: service_healthy` 使验证站不可用。 |
| 网络 | `phvalidation20261011_private`（Compose 配置为 internal）、`phvalidation20261011_edge`、`phvalidation20261011_probes` | 独立于生产 `projecthub_*` 网络。当前 `private`/`edge` 仅有 Caddy 端点；`probes` 无活动端点。 |
| 命名卷 | `phvalidation20261011_projecthub-data`、`phvalidation20261011_caddy-data`、`phvalidation20261011_caddy-config` | 均为验证项目专用，未挂载生产卷。所有者复核后如执行清理仍应保留。 |

Compose 外还存在 `agent-validation-target-20261011`：镜像 `projecthub-api:local`，命令 `sh -c sleep 3600`，状态 exited 0、restart=no、网络 `none`、无卷挂载、无端口发布。验证目录另外包含 `backups/validation-before-restore.db`（225280 字节，0600）、`bootstrap-admin.password`（49 字节，0600）、`.env`（107 字节，0600）、`agent-policy.json`（248 字节，0600）、Compose/Caddyfile、`agent.log` 和 `agent.pid`。本阶段只看文件名、权限和大小，没有读取 credential 或 policy 内容。没有单独运行的 validation Agent systemd unit；活动的 Agent 是生产 `projecthub-agent.service`。

## 用户设备浏览器验收清单

当前 CA 根指纹：`<LOCAL_CA_SHA256>`。只安装经可信通道传输且指纹匹配的公开 CA 证书；不要安装 Caddy 私钥，不要点击证书警告继续访问。

### Windows：Edge / Chrome / Firefox

- [ ] 通过可信 SSH/LAN 通道复制 `/srv/data/projecthub/certs/projecthub-local-ca.crt`，先对照上方 X.509 SHA-256 指纹。
- [ ] 运行 `certmgr.msc`，在 **Current User → Trusted Root Certification Authorities → Certificates** 导入已核验的 CA。
- [ ] 在 Edge 或 Chrome 打开 `https://<LAN_IP>/`；确认没有证书警告，并查看证书 SAN 为 `IP Address:<LAN_IP>`。
- [ ] 打开 `https://<LAN_IP>/healthz`，确认 JSON `{"status":"ok"}`。不得关闭 TLS 检查来通过验收。
- [ ] 若使用 Firefox，确认它信任 Windows 系统 CA；否则按其证书管理界面导入同一已核验根证书。

### Android：Chrome 或系统默认浏览器

- [ ] 先核验 CA 指纹，再从 Android **Security & privacy / Security → Encryption & credentials → Install a certificate → CA certificate** 导入。厂商菜单名称可能不同。
- [ ] 按系统提示设置锁屏凭据并确认 CA 已安装；只为浏览器端验收，不把用户 CA 的信任扩展推断到其它 App。
- [ ] 浏览 `https://<LAN_IP>/` 和 `/healthz`；确认没有证书错误、地址栏显示安全连接、健康 JSON 为 `ok`。
- [ ] 若出现警告，停止验收并检查 CA 指纹、安装范围、设备时间与 IP；不要点“继续访问”。

### iPhone / iPad：Safari

- [ ] 先核验公开 CA 的 X.509 SHA-256 指纹，再将证书传到设备并安装配置描述文件。
- [ ] 在 **Settings → General → About → Certificate Trust Settings** 为该根证书启用 **Full Trust**。
- [ ] Safari 打开 `https://<LAN_IP>/` 与 `/healthz`；确认没有警告，健康 JSON 为 `ok`。
- [ ] 如果设备不信任证书，移除未匹配的配置描述文件后重新核验；不得绕过警告。

所有设备完成后，记录设备型号、系统/浏览器版本、访问时间、`/healthz` 结果，以及是否出现证书警告。电脑和手机系统证书库目前没有被更改。

## 路由器 DHCP reservation 人工核对步骤

在路由器 LAN/DHCP 设置中新增静态租约/地址保留，绑定 Orange Pi 的 **Wi‑Fi MAC `<WIFI_MAC>`** 与 `<LAN_IP>`。先确认该地址属于当前 LAN 子网、未被其它设备静态占用，并遵从路由器对地址池/保留地址的规则。保存后到 DHCP 客户端列表核对 MAC、IP、主机名/租约；之后由设备所有者安排正常续租或未来受控重启，再核对地址仍为 `<LAN_IP>`。不要在验收中改路由器 WAN/端口转发/防火墙规则。

## 管理员密码轮换和会话验收

服务端密码重置已由自动化测试验证：重置会删除目标用户全部 session；有效期为 12 小时。生产环境尚未实际轮换。管理员完成以下步骤时，密码只输入 Project Hub 表单，不通过聊天或日志传递：

1. 私下读取现存 bootstrap credential 并首次登录；确认项目页面真实加载。
2. 在 **管理中心 → 用户账户** 中为当前管理员设置新的唯一强密码（至少 12 字节），使用密码管理器保存。自重置会立即撤销该账号所有会话。
3. 刷新当前页面，确认返回登录页；用新密码重新登录成功，再确认旧密码不能登录。
4. 若有其它设备保持登录，刷新其页面并确认会话失效后重新登录。无需主动登出或停止服务。
5. 确认新凭据可用后，再按管理员安全流程移除 bootstrap 密码文件 `/srv/data/projecthub/secrets/bootstrap-admin.password`。本阶段只检查了其存在和 0600 权限，未读取或删除。

## 真实项目与分类录入验收

由项目所有者使用 Admin 账号录入实际项目，不要使用占位 URL 或测试假数据：

1. 选择 **添加分类**，创建项目实际使用的分类；确认侧栏分类出现并能切换。
2. 选择 **创建项目**，填写真实名称、说明、图标（可空）、分类，并至少添加实际可访问的入口，如 Git 仓库、文档、issue tracker 或部署地址。
3. 保存后验证项目卡片、分类计数和详情显示正确；点击每个入口确认打开目标地址。
4. 重新载入页面，确认项目和分类仍在；使用搜索/分类筛选定位该项目。由 Viewer/Operator 账号确认可浏览但没有 Admin 管理操作。
5. 编辑项目说明或入口并保存，再验证结果持久化。未获用户要求时不要删除现有项目、分类或账户。

生产当前有 1 个项目、0 个分类，因此分类创建和真实入口点击需要由了解该项目的人完成。

## 备份与恢复运维方式

当前最新本地备份为 `phase12-acceptance-20261011T160412Z.db`，校验 sidecar 为 `.db.sha256`。可从另一台已有 SSH 权限的、加密存储设备上拉取备份及 sidecar，然后对照来源主机结果运行 `sha256sum -c`。该备份不应只保存在 Orange Pi 的同一 NVMe；目前**没有异机自动复制或异地备份**，因此灾难恢复目标尚未达成。配置接收端、加密密钥和保留期需要设备所有者选择并保管凭据。

隔离恢复命令已执行成功，结构如下（仅供复核，不需要对生产再次运行）：备份目录只读 bind mount，容器 `--network none`，`PROJECTHUB_DB_PATH=/var/lib/projecthub/projecthub.db` 且该目录仅为临时 tmpfs。不要将 `/srv/data/docker/volumes/projecthub_projecthub-data` 挂入演练容器。异机副本到位后，应从副本再完成一次同样的隔离恢复验证。

## 验证 Compose 资源保留清理方案

当前验证项目与生产使用分离的容器、三个独立网络和三个独立命名卷。API 失败依赖是验证专用 Agent socket 的宿主路径现为目录而不是 socket；Caddy 仅绑定回环地址，不是生产流量入口。若所有者决定清理，先再次确认这些资源没有正在使用：

```sh
docker compose -p phvalidation20261011 \
  -f /srv/data/projecthub-deploy-validation-20261011/compose.yaml \
  -f /srv/data/projecthub-deploy-validation-20261011/compose.agent.yaml \
  down
```

该方案只停止/移除该 Compose 项目的两个容器和三条 Compose 网络，**不要添加 `-v`**。它不会移除 Compose 外的 `agent-validation-target-20261011`，也不会删除验证目录的 DB、bootstrap credential、policy、日志或 pid 文件。若所有者另行确认该测试目标可移除，需单独 review 后再执行 `docker rm agent-validation-target-20261011`。不得运行 `docker volume prune` 或全局 prune。三个卷及验证目录内容都必须保留，除非日后分别授权并核对备份。Phase 12 没有执行上述清理。

## NVMe 观察及冷启动计划

- 日常只读快照：`sudo /usr/local/bin/projecthub-nvme-health-check`；需要长期趋势时由管理员决定输出位置和保留策略。当前未安装 timer/cron，也未改变 journald 配置。
- 每次未来受控冷启动前，保存 `journalctl -b -k -o short-monotonic`、`systemd-analyze critical-chain`、`findmnt /srv/data` 和健康脚本快照；重启后立刻再次读取内核单调时钟日志、`lspci -vv`、NVMe 枚举、挂载状态、Docker/API/Caddy/Agent 状态和 HTTPS 时间点。
- 由设备所有者安排至少 10 次受控冷启动，目标为 0 次 PCIe/NVMe 枚举失败、`/srv/data` 每次都在 Docker 前挂载、Project Hub 从单调时钟起算 90 秒内 HTTPS 可用；同时连续 30 天每天或每次启动保存健康快照。逐次记录 `Speed change timeout`、Root Port `DLActive`/link 状态、NVMe 出现与挂载时间、Docker/API/Caddy/Agent 可用时间。若有一次失败，保持数据盘 fail-closed 并重新诊断，不缩短挂载依赖。每次设备重启必须由设备所有者单独安排；本阶段没有执行，也没有修改设备树、速率、固件、分区或挂载配置。

## Node 版本固定依据及验证

Node.js 官方发行页将 v24.21.0 列为 2026-09-07 更新的 v24 版本，并随包提供 npm 11.19.0；项目 Dockerfile 已使用 Node 24.21.0。现在根 `.nvmrc`、前端 `engines` 和 CI 均精确指向该版本，避免开发环境落到 WSL 自带的 Node 22.22.1。CI 的 `actions/setup-node` 支持从 `.nvmrc` 读取版本。参见 [Node.js v24.21.0 官方档案](https://nodejs.org/en/download/archive/v24.21.0)、[Node.js 发布状态](https://nodejs.org/en/about/previous-releases) 和 [setup-node 版本文件说明](https://github.com/actions/setup-node/blob/main/docs/advanced-usage.md#node-version-file)。

本地取官方 Linux x64 archive 后以官方 `SHASUMS256.txt` 校验通过；Node `v24.21.0` / npm `11.19.0` 干净安装 185 个依赖，无 EBADENGINE 警告；Vitest 6/6，`npm run typecheck` 和 `npm run build` 均退出 0。浏览器/手机真实证书信任和项目录入仍需所有者操作。

开发机使用版本管理器时，在仓库根目录执行 `nvm install 24.21.0` 和 `nvm use 24.21.0`，然后确认 `node --version` 为 `v24.21.0`、`npm --version` 为 `11.19.0` 后运行 `npm ci`。CI 自动从根 `.nvmrc` 选择相同 Node 版本。
