# kore-plugin-runtime-node

Node.js 运行时插件（**基础级**）：真实检测宿主已安装的 Node、汇总单版本详情、
扫描 Node 项目并做健康检查。只读元数据——**不执行项目脚本、不写宿主文件、不内置 node 二进制**。

## 协议面

- `kore.plugin.v1`，`lifecycle=lazy`，`depth_level=basic`，能力声明 `system:admin`
- 方法：`runtime-node.versions` / `runtime-node.info` / `runtime-node.projects` / `runtime-node.doctor`
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
    {"version": "24.21.0", "path": "/home/pt/.nvm/versions/node/v24.21.0/bin/node", "source": "path", "is_default": true}
  ],
  "default_version": "24.21.0",
  "count": 1
}
```

- `source` ∈ `path` / `known_path` / `nvm`；`is_default` 由 `which node`（PATH 解析）决定，最多一个
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
  超 64 KiB 截断、坏 JSON 跳过、条目上限、去重 / 排序 / `is_default`、命令硬超时
- 真实 E2E（`tests/e2e/`）：本机真实 Node 版本与 `which node` 一致、真实 npm / V8 / OpenSSL；
  在 `/tmp/kore-node-test` 造 3 个项目（正常 / 坏 JSON / 无 package.json）→ 返回 1 个 + warnings；
  doctor 真实检查项；**真实二进制降级**：空 `PATH` + 空 `HOME` 启动插件，`versions` 返回 `count=0` + 固定 hint（不报错）
- `tests/fault/` 四类故障注入（kill / timeout / disk / cgroup）验证四个方法的协议错误码，并配 sanity 防假通过

## 构建与验收

```bash
gofmt -l . && go vet ./... && go test -timeout 200s ./...
make build && make dist
tar -tzf dist/runtime-node-0.2.0.tar.gz | head -1   # info.json
```

`dist` 使用确定性参数归档（`--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner` + `gzip -9n`），
同一源码在干净 clone 重打包 sha256 不变；`size_bytes` 由打包脚本迭代回填，与实测包体偏差 ≤2 字节。

## 许可证

MIT，见 [LICENSE](LICENSE)。
