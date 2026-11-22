# Changelog

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
