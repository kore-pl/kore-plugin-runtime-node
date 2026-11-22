# Changelog

## 0.1.0 (2026-11-22)

- feat: 新建 runtime-node 插件骨架，复用 kore.plugin.v1 握手与会话循环（提案 §2.1 批 2，基础级）
- feat: node.versions / node.allocatePort 方法级参数校验与结构化结果；版本白名单 18/20/22、端口范围 1024-65535；不真起 node 进程、不真分配端口、不内置 node 二进制
- feat: 两类基础故障注入测试（kill -9 / 超时），验证方法级错误返回并配 sanity 防假通过
- build: 确定性打包（--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner + gzip -9n），size_bytes 迭代回填偏差 ≤10%
