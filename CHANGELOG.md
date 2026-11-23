# Changelog

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
