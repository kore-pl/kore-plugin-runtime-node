# Node Runtime Plugin (runtime-node)

## What it does

Detects installed Node.js on the host, reports per-version details (npm / V8 / OpenSSL versions, global package count), scans projects and runs health checks.
Since 0.3.0 it also supports default-version pinning (for sites and the daemon) and a minimal single-process daemon (start / stop / restart / status / log tail, like a tiny pm2).
It ships no node binary, adds no resident middleware and does no container orchestration: the daemon has no supervisor process; guarded apps run detached via setsid and all state is on disk.

## Protocol and compatibility

Protocol `kore.plugin.v1`, `lifecycle=lazy`, `depth_level=basic`, `requires_kore >=0.1.0 <0.2.0`.
Plugin capabilities `system:admin` + `proc:write`; of the 13 method-level capabilities, read-only probes / lists / logs are `system:read`, version pinning and daemon config are `system:admin`, `guard.start` / `guard.stop` / `guard.restart` are `proc:write`.

## Methods

- `versions` / `info` / `projects` / `doctor` / `paths`: discovery, details, project scan, health check and PATH resolution explanation; no Node installed is not an error — an install hint is returned.
- `use {version?}`: pin an installed version as default (writes a state file, never changes host PATH); zero arguments clears the pin.
- `guard.add` / `guard.list` / `guard.start` / `guard.stop` / `guard.restart` / `guard.remove` / `guard.logs`: single-host single-process daemon, up to 32 apps; `start` is idempotent, stop sends SIGTERM to the process group then SIGKILL after 5s.

## Security boundary

The daemon uses a fixed argv (no shell), a whitelisted working directory, strips all `KORE_*` environment variables and writes logs 0640; read-only scanning never executes project scripts and caps `package.json` reads at 64 KiB; no tickers or polling — the only resident goroutine reaps exited children.

## Install and verification

`make dist` produces `dist/runtime-node-0.3.0.tar.gz` (bit-for-bit reproducible). Daily gate: `make check` (gofmt, go vet, go test -timeout 200s).
Report vulnerabilities privately per SECURITY.md; never open a public issue. MIT license; contributions: CONTRIBUTING.md.