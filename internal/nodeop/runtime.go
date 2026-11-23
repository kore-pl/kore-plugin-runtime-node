package nodeop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultKnownNodePaths 是常见安装位置（不依赖 PATH）。
var defaultKnownNodePaths = []string{
	"/usr/local/bin/node",
	"/usr/bin/node",
	"/usr/bin/nodejs",
}

// Runtime 是真实探测执行面：PATH / nvm / 常见路径 + 固定 argv 命令。
// 字段可注入，便于单测在不碰宿主环境的前提下验证去重、排序与降级逻辑。
type Runtime struct {
	runner     CommandRunner
	lookPath   func(string) (string, error)
	homeDir    func() (string, error)
	glob       func(string) ([]string, error)
	getenv     func(string) string
	knownPaths []string
}

// NewExecutor 返回生产执行面（真实探测宿主 Node 安装）。
func NewExecutor() Executor { return newRuntime() }

func newRuntime() *Runtime {
	return &Runtime{
		runner:     osRunner{timeout: CommandTimeout},
		lookPath:   exec.LookPath,
		homeDir:    os.UserHomeDir,
		glob:       filepath.Glob,
		getenv:     os.Getenv,
		knownPaths: defaultKnownNodePaths,
	}
}

// osRunner 是 CommandRunner 的生产实现：无 shell、固定 argv、硬超时。
type osRunner struct {
	timeout time.Duration
}

// Run 执行一条命令并返回去空白的 stdout；超时与退出码非 0 都返回错误。
func (r osRunner) Run(name, pathDir string, args ...string) (string, error) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = CommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = envWithPath(pathDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("命令 %s 超时（%s）", filepath.Base(name), timeout)
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("命令 %s 失败: %w（stderr: %s）",
			filepath.Base(name), err, trimForMessage(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// envWithPath 构造子进程环境：pathDir 非空时替换 PATH 并把该目录前置，
// 让 npm 的 shebang 使用配套 node，而不是当前 shell 里碰巧排在前面的另一个 node。
func envWithPath(pathDir string) []string {
	current := os.Getenv("PATH")
	if pathDir != "" {
		current = pathDir + string(os.PathListSeparator) + current
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "PATH="+current)
}

// trimForMessage 截断过长的 stderr，避免错误信息膨胀。
func trimForMessage(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > 200 {
		return trimmed[:200] + "…"
	}
	return trimmed
}

// candidate 是一个待验证的 node 可执行文件候选。
type candidate struct {
	path   string
	source Source
}

// Versions 探测宿主全部 Node 安装并按「默认优先 + 版本降序 + 路径升序」返回。
// 任一候选执行 --version 失败只跳过该候选，不影响其他安装。
func (r *Runtime) Versions() (VersionsResult, error) {
	versions := r.detect()
	defaultVersion := ""
	for _, item := range versions {
		if item.IsDefault {
			defaultVersion = item.Version
			break
		}
	}
	result := VersionsResult{Versions: versions, DefaultVersion: defaultVersion, Count: len(versions)}
	if len(versions) == 0 {
		result.Hint = NoNodeHint
	}
	return result, nil
}

// detect 收集候选、验版本、按真实路径去重后排序。
func (r *Runtime) detect() []NodeVersion {
	candidates := r.candidates()
	seen := map[string]bool{}
	versions := make([]NodeVersion, 0, len(candidates))
	for _, item := range candidates {
		real, ok := resolveCandidate(item.path)
		if !ok || seen[real] {
			continue
		}
		version, err := r.nodeVersion(item.path)
		if err != nil {
			continue
		}
		seen[real] = true
		binDir := filepath.Dir(item.path)
		versions = append(versions, NodeVersion{
			Version: version, Path: item.path, Source: item.source,
			BinDir:             binDir,
			PackageManager:     PackageManager,
			PackageManagerPath: r.packageManagerPath(binDir),
		})
	}
	defaultReal := r.defaultRealPath()
	for i := range versions {
		if defaultReal != "" {
			if real, ok := resolveCandidate(versions[i].Path); ok && real == defaultReal {
				versions[i].IsDefault = true
			}
		}
	}
	sortVersions(versions)
	return versions
}

// candidates 按优先级收集候选：PATH → nvm → 常见路径 → /opt/node*。
func (r *Runtime) candidates() []candidate {
	out := make([]candidate, 0, 8)
	seen := map[string]bool{}
	add := func(path string, source Source) {
		cleaned := filepath.Clean(strings.TrimSpace(path))
		if cleaned == "" || seen[cleaned] {
			return
		}
		seen[cleaned] = true
		out = append(out, candidate{path: cleaned, source: source})
	}
	for _, name := range runtimeBinaryNames {
		if path, err := r.lookPath(name); err == nil {
			add(path, SourcePath)
		}
	}
	if home, err := r.homeDir(); err == nil && home != "" {
		if matches, globErr := r.glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node")); globErr == nil {
			for _, match := range matches {
				add(match, SourceNVM)
			}
		}
	}
	for _, path := range r.knownPaths {
		add(path, SourceKnownPath)
	}
	if matches, err := r.glob("/opt/node*/bin/node"); err == nil {
		for _, match := range matches {
			add(match, SourceKnownPath)
		}
	}
	return out
}

// resolveCandidate 跟随符号链接返回真实路径，并确认目标是可执行文件（普通文件）。
func resolveCandidate(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, true
	}
	return filepath.Clean(path), true
}

// defaultRealPath 按 `which node` 语义（PATH 解析）返回默认 node 的真实路径。
func (r *Runtime) defaultRealPath() string {
	path, err := r.lookPath("node")
	if err != nil || path == "" {
		return ""
	}
	if real, ok := resolveCandidate(path); ok {
		return real
	}
	return ""
}

// nodeVersion 执行 `<node> --version` 并归一化版本号。
func (r *Runtime) nodeVersion(path string) (string, error) {
	out, err := r.runner.Run(path, "", "--version")
	if err != nil {
		return "", err
	}
	version, ok := normalizeVersion(out)
	if !ok {
		return "", fmt.Errorf("无法解析 %s --version 输出: %q", path, out)
	}
	return version, nil
}

// nodeExpr 执行 `<node> -p <expr>`，失败返回空串（详情字段降级，不放大为整次失败）。
func (r *Runtime) nodeExpr(entry NodeVersion, expr string) string {
	out, err := r.runner.Run(entry.Path, "", "-p", expr)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Info 汇总单个版本的详情；version 为空时取默认版本。
func (r *Runtime) Info(version string) (InfoResult, error) {
	versions := r.detect()
	if len(versions) == 0 {
		return InfoResult{}, ErrNodeNotInstalled
	}
	entry, ok := selectVersion(versions, version)
	if !ok {
		return InfoResult{}, fmt.Errorf("%w: %s（已检测: %s）", ErrVersionNotInstalled, version, joinVersionList(versions))
	}
	nodeDir := filepath.Dir(entry.Path)
	npmPath, npmVersion := r.probeNpm(entry, nodeDir)
	result := InfoResult{
		Version:    entry.Version,
		NodePath:   entry.Path,
		NpmPath:    npmPath,
		NpmVersion: npmVersion,
		V8:         r.nodeExpr(entry, "process.versions.v8"),
		OpenSSL:    r.nodeExpr(entry, "process.versions.openssl"),
	}
	if npmPath != "" {
		result.GlobalPackages = r.globalPackageCount(npmPath, nodeDir)
	}
	return result, nil
}

// selectVersion 选取请求版本；空请求优先默认版本，其次第一个。
func selectVersion(versions []NodeVersion, version string) (NodeVersion, bool) {
	if version == "" {
		for _, item := range versions {
			if item.IsDefault {
				return item, true
			}
		}
		if len(versions) > 0 {
			return versions[0], true
		}
		return NodeVersion{}, false
	}
	for _, item := range versions {
		if item.Version == version {
			return item, true
		}
	}
	return NodeVersion{}, false
}

// probeNpm 返回与 node 配套的 npm 路径与版本：优先同目录 npm，其次 PATH（which npm）。
func (r *Runtime) probeNpm(entry NodeVersion, nodeDir string) (string, string) {
	npmPath := ""
	sibling := filepath.Join(nodeDir, "npm")
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		npmPath = sibling
	} else if path, err := r.lookPath("npm"); err == nil {
		npmPath = path
	}
	if npmPath == "" {
		return "", ""
	}
	out, err := r.runner.Run(npmPath, nodeDir, "--version")
	if err != nil {
		return npmPath, ""
	}
	return npmPath, firstLine(out)
}

// globalPackageCount 解析 `npm ls -g --depth=0 --json` 的顶层依赖数；失败返回 0。
func (r *Runtime) globalPackageCount(npmPath, nodeDir string) int {
	out, err := r.runner.Run(npmPath, nodeDir, "ls", "-g", "--depth=0", "--json")
	if err != nil && strings.TrimSpace(out) == "" {
		return 0
	}
	return parseNpmGlobalCount(out)
}

// parseNpmGlobalCount 统计 npm 全局清单 JSON 的顶层依赖键数。
func parseNpmGlobalCount(raw string) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	var list npmGlobalList
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return 0
	}
	return countObjectKeys(list.Dependencies)
}

// countObjectKeys 统计 JSON 对象的顶层键数；非对象/空值返回 0。
// 依赖名是动态键，这里用 map[string]json.RawMessage 仅做解析计数（值保持原始 JSON，不引入 any）。
func countObjectKeys(raw json.RawMessage) int {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return 0
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return 0
	}
	return len(object)
}

// packageManagerPath 返回与当前 node 配套的包管理器路径：同目录优先，其次 PATH。
// 只做文件查找（os.Stat / exec.LookPath），不执行任何命令。
func (r *Runtime) packageManagerPath(binDir string) string {
	for _, name := range packageManagerNames {
		sibling := filepath.Join(binDir, name)
		if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
			return sibling
		}
	}
	for _, name := range packageManagerNames {
		if path, err := r.lookPath(name); err == nil && path != "" {
			return path
		}
	}
	return ""
}

// Paths 返回 PATH 解析、默认 Node 解析链与按优先级排列的发现来源。
// 只读：复用 candidates()/lookPath/glob 与一次 `--version`，不修改宿主状态。
func (r *Runtime) Paths() (PathsResult, error) {
	result := PathsResult{
		PathEnv:     splitPathEnv(r.getenv("PATH")),
		Executables: make([]PathExecutable, 0, len(runtimeBinaryNames)),
		SearchOrder: make([]SearchSource, 0, 4),
	}
	for _, name := range runtimeBinaryNames {
		entry := PathExecutable{Name: name}
		if path, err := r.lookPath(name); err == nil && path != "" {
			entry.Path = path
			entry.Found = true
		}
		result.Executables = append(result.Executables, entry)
	}
	result.Default = r.defaultRuntime()
	if !result.Default.Found {
		result.Hint = NoNodeHint
	}
	result.SearchOrder = r.searchOrder()
	return result, nil
}

// defaultRuntime 解析当前默认 Node（which node 语义）并补齐版本与包管理器。
func (r *Runtime) defaultRuntime() DefaultRuntime {
	path, err := r.lookPath("node")
	if err != nil || path == "" {
		return DefaultRuntime{Command: "node"}
	}
	entry := DefaultRuntime{Command: "node", Path: path, Found: true}
	if real, ok := resolveCandidate(path); ok {
		entry.RealPath = real
	}
	if version, verr := r.nodeVersion(path); verr == nil {
		entry.Version = version
	}
	entry.BinDir = filepath.Dir(path)
	entry.PackageManager = PackageManager
	entry.PackageManagerPath = r.packageManagerPath(entry.BinDir)
	return entry
}

// searchOrder 把 candidates() 按来源分组（保持探测顺序），给出去重后的命中路径。
func (r *Runtime) searchOrder() []SearchSource {
	order := make([]SearchSource, 0, 4)
	index := map[Source]int{}
	for _, item := range r.candidates() {
		if position, ok := index[item.source]; ok {
			order[position].Paths = append(order[position].Paths, item.path)
			continue
		}
		index[item.source] = len(order)
		order = append(order, SearchSource{Source: item.source, Paths: []string{item.path}})
	}
	return order
}

// splitPathEnv 按平台分隔符拆分 PATH，丢弃空项（保留原始顺序）。
func splitPathEnv(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// normalizeVersion 把 `v18.19.0` / `18.19.0` 归一为 `18.19.0`；非法格式返回 false。
func normalizeVersion(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "v") && !strings.HasPrefix(trimmed, "V") {
		trimmed = "v" + trimmed
	}
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(trimmed, "v"), "V"), ".")
	if len(parts) != 3 {
		return "", false
	}
	for _, part := range parts {
		if part == "" {
			return "", false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return "", false
		}
	}
	return strings.Join(parts, "."), true
}

// compareVersions 比较两个 x.y.z 版本号；无法解析时退回字符串比较。
func compareVersions(a, b string) int {
	left, leftOK := versionTriple(a)
	right, rightOK := versionTriple(b)
	if !leftOK || !rightOK {
		return strings.Compare(a, b)
	}
	for i := 0; i < 3; i++ {
		if left[i] != right[i] {
			if left[i] < right[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionTriple 解析 x.y.z 为三个整数。
func versionTriple(version string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return out, false
		}
		out[i] = value
	}
	return out, true
}

// sortVersions 默认版本排最前，其次版本降序，最后按路径升序（保证结果确定）。
func sortVersions(versions []NodeVersion) {
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].IsDefault != versions[j].IsDefault {
			return versions[i].IsDefault
		}
		if cmp := compareVersions(versions[i].Version, versions[j].Version); cmp != 0 {
			return cmp > 0
		}
		return versions[i].Path < versions[j].Path
	})
}

// firstLine 取多行输出的第一行。
func firstLine(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		return strings.TrimSpace(trimmed[:idx])
	}
	return trimmed
}

// joinVersionList 拼接已安装版本号，用于错误信息里的可重试取值。
func joinVersionList(versions []NodeVersion) string {
	out := make([]string, 0, len(versions))
	for _, item := range versions {
		out = append(out, item.Version)
	}
	if len(out) == 0 {
		return "无"
	}
	return strings.Join(out, ",")
}
