# kore-plugin-runtime-node

Node.js 运行时插件（**基础级**）：真实检测宿主已安装的 Node、汇总单版本详情、
扫描 Node 项目并做健康检查。只读元数据——**不执行项目脚本、不写宿主文件、不内置 node 二进制**。

## 协议面

- `kore.plugin.v1`，`lifecycle=lazy`，`depth_level=production`，插件级能力 `system:admin`（仅作未声明方法的 fail-closed 回退）
- **方法级能力**（`method_capabilities`）：5 个方法全为 `system:read`——只读探测无需管理权限，内核据此不再把只读调用计入写审计
- 方法：`runtime-node.versions` / `runtime-node.info` / `runtime-node.projects` / `runtime-node.doctor` / `runtime-node.paths`
  （域必须等于插件 id `runtime-node`，PLUGIN-PROTOCOL §7.2；不接受裸动词或旧 `node.` 前缀）
- 握手的 stdout 留给协议帧，日志一律写 stderr；未知方法 `E_NOTFOUND`，参数非法 `E_INVALID`

## 方法

### `runtime-node.versions`（无参数）

探测宿主全部 Node 安装：PATH 里的 `node` / `nodejs`、`~/.nvm/versions/node/*/bin/node`、
`/usr/local/bin/node`、`/usr/bin/node`、`/usr/bin/nodejs`、`/opt/node*/bin/node`。
每项执行 `<path> --version` 并把 `v18.19.0` 归一为 `18.19.0`；同一真实文件按符号链接去重。

```json
{
  "versions": [
    {"version": "24.21.0", "path": "/home/pt/.nvm/versions/node/v24.21.0/bin/node", "source": "path",
     "is_default": true, "bin_dir": "/home/pt/.nvm/versions/node/v24.21.0/bin",
     "package_manager": "npm", "package_manager_path": "/home/pt/.nvm/versions/node/v24.21.0/bin/npm"}
  ],
  "default_version": "24.21.0",
  "count": 1
}
```

- `source` ∈ `path` / `known_path` / `nvm`；`is_default` 由 `which node`（PATH 解析）决定，最多一个
- `bin_dir` 是安装目录；`package_manager` 固定 `npm`；`package_manager_path` 优先同目录 `npm`、其次 PATH 上的 `npm`
  （**只做文件查找，不执行命令**，因此不会为每个版本额外拉起进程）
- **未检测到 Node 不报错**：`versions=[]`、`count=0`、`default_version=""`，并给固定 `hint`：
  `未检测到 Node.js，可从 https://nodejs.org 安装或用 nvm`

### `runtime-node.info {version?}`

`version` 省略时取默认版本；给出时必须是已安装版本（`v18.19.0` 与 `18.19.0` 都接受）。
执行 `node --version`、配套 `npm --version`、`node -p process.versions.v8`、
`node -p process.versions.openssl`，并用 `npm ls -g --depth=0 --json` 统计全局包数（失败为 0）。

```json
{
  "version": "24.21.0",
  "node_path": "/home/pt/.nvm/versions/node/v24.21.0/bin/node",
  "npm_path": "/home/pt/.nvm/versions/node/v24.21.0/bin/npm",
  "npm_version": "11.19.0",
  "v8": "13.6.233.17-node.53",
  "openssl": "3.5.8",
  "global_packages": 7
}
```

单版本未安装返回 `E_INVALID`（message 列出已检测版本）；宿主完全没有 Node 返回 `E_NOTFOUND` + 安装提示。
npm/V8/OpenSSL 单项探测失败只让对应字段为空串，不放大为整次失败。

### `runtime-node.projects {root}`

扫描 `root` 下**深度 ≤3**、**条目 ≤200** 的目录，收集含 `package.json` 的 Node 项目（含 root 自身）；
跳过 `node_modules`、`.git`、隐藏目录，不跟随目录符号链接。

```json
{
  "projects": [
    {"path": "/tmp/kore-node-test/good-app", "name": "good-app", "version": "1.2.3",
     "engines_node": ">=18", "dependencies_count": 2, "has_lock": true}
  ],
  "count": 1,
  "warnings": ["package.json 解析失败，已跳过: /tmp/kore-node-test/bad-json/package.json"]
}
```

- `package.json` **只读前 64 KiB**；超限 / 坏 JSON / 非对象一律跳过并写 `warnings[]`，不中断整次扫描
- `has_lock` 命中 `package-lock.json` / `npm-shrinkwrap.json` / `yarn.lock` / `pnpm-lock.yaml`
- **不执行**项目的任何脚本，只读元数据

### `runtime-node.doctor {root?}`

真实检查：Node 是否安装、npm 是否可用、`npm root -g` 全局包目录是否可写、`NODE_ENV`、
多版本 PATH 冲突；给出 `root` 时追加项目目录检查。`ok = 没有任何 fail 项`（warn 不拉低 ok）。

```json
{
  "ok": true,
  "checks": [
    {"name": "node", "status": "pass", "detail": "检测到 1 个版本，默认 24.21.0（…/bin/node）"},
    {"name": "npm", "status": "pass", "detail": "npm 11.19.0（…/bin/npm）"},
    {"name": "global_packages_writable", "status": "pass", "detail": "全局包目录可写: …（mode=0755）"},
    {"name": "node_env", "status": "warn", "detail": "NODE_ENV 未设置；生产环境建议 export NODE_ENV=production"}
  ]
}
```

### `runtime-node.paths`（无参数）

解释「**为什么用到了这个版本**」：PATH 原始顺序、候选可执行名解析结果、默认 Node 的解析链，
以及按探测优先级排列的发现来源。只读：复用 `versions` 的同一套候选收集逻辑与一次 `--version`，不写宿主。

```json
{
  "path_env": ["/usr/local/sbin", "/usr/local/bin", "/usr/bin"],
  "executables": [
    {"name": "node", "path": "/usr/bin/node", "found": true},
    {"name": "nodejs", "path": "", "found": false}
  ],
  "default": {
    "command": "node",
    "path": "/usr/bin/node",
    "real_path": "/usr/bin/node",
    "version": "18.19.0",
    "bin_dir": "/usr/bin",
    "package_manager": "npm",
    "package_manager_path": "/usr/bin/npm",
    "found": true
  },
  "search_order": [
    {"source": "path", "paths": ["/usr/bin/node"]},
    {"source": "nvm", "paths": ["/root/.nvm/versions/node/v20.11.0/bin/node"]},
    {"source": "known_path", "paths": ["/usr/bin/node"]}
  ]
}
```

- `search_order` 的条目顺序 = 实际探测优先级（PATH → nvm → 常见路径），只列**真实存在**的来源；同一路径只列一次（软链仍按来源保留，便于解释多版本来源）
- 未检测到 Node 时不报错：`default.found=false`、`search_order=[]`，并给与 `versions` 相同的 `hint`

## 安全

- **root 白名单**：`/var/www`、`/opt`、`/home` 与插件状态目录（`KORE_NODE_ROOT`，默认 `/var/lib/kore/node`）；
  必须是绝对路径、无 NUL / 反斜杠 / `..`；`ResolveExisting`（`EvalSymlinks` + 存在性 + 目录）后再比对归属，
  挡住父目录被换成软链的逃逸
- **只读**：不执行项目脚本、不写任何宿主文件；`package.json` 读取上限 64 KiB
- **命令固定 argv + 5s 超时**：`node` / `npm` 一律走 `exec.CommandContext`，无 shell；npm 子进程 `PATH`
  前置该 node 所在目录，保证 shebang 使用配套 node
- **零轮询**：无 ticker / 后台 goroutine，全部探测都在单次请求内完成

## 测试

- 单元：版本解析（`v18.19.0 → 18.19.0`）、路径白名单与软链逃逸、`package.json` 解析（含 `engines.node`）、
  超 64 KiB 截断、坏 JSON 跳过、条目上限、去重 / 排序 / `is_default`、命令硬超时；
  `versions` 增强字段（`bin_dir` / `package_manager` / `package_manager_path` 同目录优先与 PATH 回退）、
  `paths`（PATH 顺序、默认解析链、来源顺序与 `candidates()` 一致性）、参数严格校验；
  `capability_test` 双向校验 `info.json` 的 `method_capabilities` 与分发表
- 真实 E2E（`tests/e2e/`）：本机真实 Node 版本与 `which node` 一致、真实 npm / V8 / OpenSSL；
  在 `/tmp/kore-node-test` 造 3 个项目（正常 / 坏 JSON / 无 package.json）→ 返回 1 个 + warnings；
  doctor 真实检查项；**真实二进制降级**：空 `PATH` + 空 `HOME` 启动插件，`versions` 返回 `count=0` + 固定 hint（不报错）
- `tests/fault/` 四类故障注入（kill / timeout / disk / cgroup）验证五个方法的协议错误码，并配 sanity 防假通过

## 构建与验收

```bash
gofmt -l . && go vet ./... && go test -timeout 200s ./...
make build && make dist
tar -tzf dist/runtime-node-0.2.3.tar.gz | head -1   # info.json
```

`dist` 使用确定性参数归档（`--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner` + `gzip -9n`），
同一源码在干净 clone 重打包 sha256 不变；`size_bytes` 由打包脚本迭代回填，与实测包体偏差 ≤2 字节。

## 许可证

MIT，见 [LICENSE](LICENSE)。

<!-- oss:begin -->
## 开源与贡献

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)

本仓库是 **kore** 的 `Node 运行时插件`（id `runtime-node`），独立仓库、独立版本线。内核与协议见 [kore](https://github.com/kore-pl/kore)。

- **许可**：MIT（inbound = outbound，不要求 CLA）；提交用 `git commit -s` 签 DCO。
- **贡献**：[CONTRIBUTING.md](CONTRIBUTING.md)（构建、门禁、发版、边界）。
- **安全**：[SECURITY.md](SECURITY.md)——漏洞不要开公开 issue。
- **行为准则**：[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。
- **兼容性**：`protocol: kore.plugin.v1` · `requires_kore: >=0.1.0 <0.2.0` · `lifecycle: lazy` · 深度等级 `production`。
- **验证方式**：本地 `make check`（插件仓不配 CI；发布前 `make dist` 连跑两次核对 sha 一致）。
- **发布产物**：`make dist` → `dist/runtime-node-0.2.3.tar.gz` + `.sha256`（位级可复现，连跑两次 sha 相同）。
- **相关仓库**：[索引 kore-plugins](https://github.com/kore-pl/kore-plugins) · [协议文档](https://github.com/kore-pl/kore/blob/main/docs/PLUGIN-PROTOCOL.md) · [SDK 指南](https://github.com/kore-pl/kore/blob/main/docs/PLUGIN-SDK.md)

<!-- oss:end -->
