# kore-plugin-runtime-node

Node 运行时插件（**基础级骨架**）：覆盖 Node 进程守护 / 端口分配的方法面。本仓只交付协议骨架与参数校验，**不真起 node 进程、不真分配端口、不内置任何 node 二进制**（提案 §2.1 批 2）。

## 协议面

- `kore.plugin.v1`，`lifecycle=lazy`，`depth_level=basic`
- 能力声明：`system:admin`
- 方法：`node.versions` / `node.allocatePort`（同时接受裸 `versions` / `allocatePort`）
- 握手的 stdout 留给协议帧，日志一律写 stderr

## 方法口径（骨架）

- `node.versions`：返回支持的 Node 版本白名单 `18 / 20 / 22`；`family` 可省略，给出时只接受 `node`。不查询宿主 node 安装情况。
- `node.allocatePort`：`preferred` 可省略（0，由调度侧决定）；给出时必须在 `1024-65535` 内。结果结构含 `allocated` 字段，**骨架口径恒为 false**，不执行任何 bind / 端口占用。

两个方法都只做参数校验 + 结构化结果；非法参数返回 `E_INVALID`，未知方法返回 `E_NOTFOUND`。参数必须是 JSON 对象，未知字段一律拒绝。

## 基础故障注入（basic 级）

`tests/fault/` 两个文件分别注入 kill -9 与调用超时，断言方法级返回结构化错误：

| 故障 | 文件 | 期望错误码 |
| --- | --- | --- |
| kill -9 | `kill_test.go` | `E_UPSTREAM` |
| 超时 | `timeout_test.go` | `E_UPSTREAM` |

每个类别都配 sanity 用例（无故障时应正常返回），防止"永远失败"的假通过。本仓为基础级，磁盘满 / cgroup 两类边界用例延 v1.1（提案 §2.1）。

## 构建与验收

```bash
gofmt -l . && go vet ./... && go test -timeout 200s ./...
make build && make dist
tar -tzf dist/runtime-node-0.1.0.tar.gz | head -1   # info.json
```

`dist` 使用确定性参数归档（`--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner` + `gzip -9n`），重复构建 sha256 不变；`size_bytes` 由打包脚本迭代回填，与实际包体偏差 ≤10%。

## 许可证

MIT，见 [LICENSE](LICENSE)。
