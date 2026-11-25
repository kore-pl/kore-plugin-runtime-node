# Changelog

## [0.3.1] - 2026-11-25

### Docs / 配置（patch）

- 完善开源工程标准化：新增简体中文 / English / Español / Français / Deutsch / Русский / Português 七语言 DETAIL 文档与 locale 元数据，新增 SECURITY 六语译文、dependabot 与 CODEOWNERS，README 顶部加语言切换行与 Star 提示。
- 补充 Git Flow 分支模型与安全披露约定，GitHub CI 改为仅手动触发、新增 Gitea 按版本 tag 触发。
- 本地签名 tag v0.3.1（工程标准化，不改运行时版本）；历史 tag 不改动、不重签、不推送。

## [0.3.0] - 2026-11-25

### Added

- **进程守护（极简 pm2 同款能力，不做编排）**：`guard.add` / `guard.list` / `guard.start` /
  `guard.stop` / `guard.restart` / `guard.remove` / `guard.logs`。启动用 `<node> <script> <args...>`
  拉起脱离会话（setsid）的子进程，stdout/stderr 追加到 `<state>/guard/<name>.log`；
  停止先给**进程组** SIGTERM、5s 不退升级 SIGKILL；状态含 running / stopped / crashed、PID、运行时长、RSS、
  日志大小与尾部 500 行。**没有 supervisor 常驻进程**（G-05 无中间件、G-19 不自起 ticker）：
  插件 lazy 拉起、回完响应即退出，被守护进程独立存活；`guard.start` 幂等，零参数 = 全部启动。
- **默认版本切换**：`use {version?}` 把已安装版本钉为默认（写 `<state>/default-version.json`，不改宿主 PATH），
  零参数 = 清除钉住；`versions` 新增 `pinned_version` / `pinned_path`，与 `default_version`（PATH 解析）分开呈现。

### Changed

- 插件级能力 `["system:admin"]` → `["system:admin", "proc:write"]`：守护真的启停本机进程，
  方法级能力按内核一致性规则（只能收窄、不能跨域放大）需要该域在插件级声明；
  `method_capabilities` 5 条 → 13 条（guard 启停 = `proc:write`，配置增删与 use = `system:admin`，其余只读）。

### Fixed

- **僵尸进程被误判为运行中**（真机验证发现）：父进程活着时已退出的子进程在 `/proc` 里是 `Z` 状态，
  原先只判 PID 存在 → 停止后仍显示 running。现在拉起后用一个阻塞在 `Wait` 上的回收 goroutine
  （事件驱动、退出即结束，不是 ticker）回收子进程并把退出码落盘，`Probe` 同时把 `Z` / `X` 判为不在运行；
  状态页因此能显示「已退出（退出码 N）」。

## [0.2.5] - 2026-11-24

### Changed

- **`depth_level`: `production` → `basic`**. Upgrade criterion ① in `kore/docs/PLUGIN-CATALOG.md` §6.1 (boundary cases: invalid input / concurrency / insufficient permission / resource exhaustion, each ≥1 case wired into CI) is **not met** — this repo has no `tests/boundary/` and `grep -rn "t.Parallel" tests/` returns 0. Fault injection (criterion ②) passes, but both must hold; the previous `production` claim was an **over-declaration** and the index value `basic` is correct. **Not a regression defect**: no code, capability or method-contract change; production can be re-claimed once boundary cases land.

## [0.2.4] - 2026-11-23

### Changed

- 测试解耦版本号：`main_test.go` 不再硬编码发布版本字面量，改为断言 `version` 符合
  MAJOR.MINOR.PATCH 且内嵌清单、`info.json`、`manifest.json` 三处同步（Gitea dns#1 技术债），
  此后 bump 无需改测试；本次 bump 仅版本号变化，方法契约与行为不变。

## [0.2.3] - 2026-11-23

### Changed
- 提升为 `production` 深度等级（故障注入测试已覆盖）

## 0.2.2 (2026-11-23)

- fix: `runtime-node.paths` 的两处口径修正（原 0.2.1 已发布，按「改 dist sha 必须 bump」升 patch）：
  1. `hint` 语义——装了 Node 但 PATH 上没有 `node` 时不再误报「未检测到 Node.js」，改为说明
     「已安装的 Node 仍在 versions 列表中，但无法确定默认版本，请检查 PATH」；确实一个都没装才给安装提示
  2. `search_order` 只列**真实存在**的来源——固定已知路径（`/usr/bin/nodejs` 等）不存在时不再出现在命中列表
- test: 空 PATH 二进制降级用例断言由「恒为安装提示」改为按「有无安装」区分两种 hint；单测覆盖两条路径
- docs: README 修正 `search_order` 的去重口径（同一路径只列一次，软链按来源保留）


## 0.2.1 (2026-11-23)

- feat: 方法级能力声明（§3.5 #12）——`runtime-node.versions` / `info` / `projects` / `doctor` / `paths` 全部 `system:read`，
  只读方法不再按插件级 `system:admin` 计入写审计；插件级能力保持不变，作为未声明方法的 fail-closed 回退
- feat: `runtime-node.versions` 增强元信息——每项新增 `bin_dir`（安装目录）、`package_manager`（固定 `npm`）、
  `package_manager_path`（同目录 npm 优先、其次 PATH，**只做文件查找不执行命令**）
- feat: 新增 `runtime-node.paths`——返回 `path_env`（PATH 原始顺序）、`executables`（node/nodejs 解析结果）、
  `default`（which node 解析链 + 版本 + 包管理器）与 `search_order`（按探测优先级列出的命中来源），
  用于排查「为什么用到了这个版本」；未检测到 Node 时 `default.found=false` + 固定 hint，不报错
- test: 新增 capability 双向校验（声明 vs 分发表 vs 内核键格式/值域）、增强字段与 `paths` 单测、
  二进制空 PATH 降级链路补 `paths` 断言；故障注入扩到五个方法
- docs: README 补 5 方法契约、增强字段与 `paths` 口径、方法级能力说明
- note: **进程守护 / 扩展管理本轮不做**——两者都是写操作且需要 root（装包、改全局目录、托管进程需 systemd/root 通道），
  必须走 `root_actions` + 不可逆确认，另立任务实现；本轮只交付只读增强
- build: 版本 0.2.1（只加元数据 + 只读增强 → patch），`info.json` / `manifest.json` 同步


## 0.2.0 (2026-11-23)

- feat: `runtime-node.versions` 改为**真实检测**宿主 Node 安装（PATH 的 node/nodejs、nvm、常见路径、`/opt/node*`），
  归一化版本号、按真实路径去重、`is_default` 由 `which node` 决定；未检测到 Node 时返回 `count=0` + 固定 hint，不报错
- feat: 新增 `runtime-node.info`：真实 `node/npm --version`、V8、OpenSSL 与 `npm ls -g --depth=0 --json` 全局包数
- feat: 新增 `runtime-node.projects`：扫描 root（深度 ≤3 / 条目 ≤200）下的 Node 项目，
  返回 name/version/engines_node/dependencies_count/has_lock；`package.json` 只读前 64 KiB，坏 JSON 跳过并写 warnings
- feat: 新增 `runtime-node.doctor`：真实检查 Node / npm / `npm root -g` 可写 / NODE_ENV / 多版本冲突（可选项目目录检查）
- security: root 白名单（`/var/www`、`/opt`、`/home`、`<state>`）+ `ResolveExisting` + 符号链接二次归属校验；
  外部命令固定 argv + 5s 超时；不执行项目脚本、只读元数据
- refactor: 移除骨架方法 `runtime-node.allocatePort` 与 `noopExecutor`（0.2.0 方法面为 versions / info / projects / doctor）
- test: 真实 E2E（本机 Node/npm/V8、`/tmp/kore-node-test` 三项目、二进制空 PATH 降级路径）；
  `tests/fault/` 扩到四类故障注入（kill / timeout / disk / cgroup）

## 0.1.1 (2026-11-23)

- refactor: 方法名统一为 runtime-node.<action>，对齐协议域校验
- build: 位级可复现打包（stage chmod + tar --mode 归一、size_bytes 收敛），二进制改嵌 manifest.json，消除同版本 sha 漂移

## 0.1.0 (2026-11-22)

- feat: 新建 runtime-node 插件骨架，复用 kore.plugin.v1 握手与会话循环（提案 §2.1 批 2，基础级）
- feat: runtime-node.versions / runtime-node.allocatePort 方法级参数校验与结构化结果；版本白名单 18/20/22、端口范围 1024-65535；不真起 node 进程、不真分配端口、不内置 node 二进制
- refactor: 方法名按协议统一为 `<plugin-id>.<action>`（原 `node.*` → `runtime-node.*`），移除裸动词与旧域前缀兼容
- feat: 两类基础故障注入测试（kill -9 / 超时），验证方法级错误返回并配 sanity 防假通过
- build: 确定性打包（--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner + gzip -9n），size_bytes 迭代回填偏差 ≤10%

## [v0.3.1] - 2026-11-25

### Added

- 补齐开源工程标准化：新增简体中文 / English / Español / Français / Deutsch / Русский / Português 七语言文档（`locale/` 与 `docs/DETAIL*`）、安全与依赖更新配置（dependabot / CODEOWNERS / SECURITY 译文）、贡献指南的分支模型说明，以及中文 CHANGELOG 语义化版本条目。
- 新增本地 `develop` 分支约定与 SSH 签名 tag 约定（历史 tag 不动）。

### Changed

- README 增加七语言切换行与 Star 支持提示；对应版本标签 `v0.3.1` 仅作为工程标准化发布标识，`info.json` / `manifest.json` 的运行时版本与 `dist` 产物保持不变，避免发布包 sha 漂移。
