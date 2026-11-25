# Node 运行时插件（runtime-node）

## 它做什么

真实检测宿主已安装的 Node.js，汇总单版本详情（npm / V8 / OpenSSL 版本、全局包数），扫描项目并做健康检查。
0.3.0 起补**默认版本切换**（把已装版本钉给站点与守护用）与**单进程守护**（启动 / 停止 / 重启 / 状态 / 日志尾部，类似 pm2 的极简版）。
不内置 node 二进制、不引入常驻中间件、不做容器编排：守护没有 supervisor 进程，被守护应用 setsid 脱离会话独立运行，状态全部落盘。

## 协议与兼容性

协议 `kore.plugin.v1`，`lifecycle=lazy`，`depth_level=basic`，`requires_kore >=0.1.0 <0.2.0`。
插件级能力 `system:admin` + `proc:write`；13 条方法级能力中只读探测 / 列表 / 日志为 `system:read`，版本钉住与守护配置增删为 `system:admin`，`guard.start` / `guard.stop` / `guard.restart` 为 `proc:write`。

## 方法

- `versions` / `info` / `projects` / `doctor` / `paths`：探测、详情、项目扫描、健康检查与 PATH 解析解释；未检测到 Node 不报错，给安装提示。
- `use {version?}`：把已装版本钉为默认（写状态文件，不改宿主 PATH）；零参数清除钉住。
- `guard.add` / `guard.list` / `guard.start` / `guard.stop` / `guard.restart` / `guard.remove` / `guard.logs`：单机单进程守护，上限 32 个应用；`start` 幂等，停止先 SIGTERM 进程组、5s 不退升级 SIGKILL。

## 安全边界

守护拉起固定 argv（无 shell）、工作目录限定白名单、环境变量剔除全部 `KORE_*`、日志 0640；只读扫描不执行项目脚本，`package.json` 读取上限 64 KiB；无 ticker / 轮询，唯一常驻 goroutine 是子进程退出回收。

## 安装与验证

插件包由 `make dist` 产出 `dist/runtime-node-0.3.0.tar.gz`（位级可复现）。日常门禁 `make check`（gofmt → go vet → go test -timeout 200s）。
漏洞按 [SECURITY.md](SECURITY.md) 私下披露，不要开公开 issue。MIT 许可；贡献见 [CONTRIBUTING.md](CONTRIBUTING.md)。