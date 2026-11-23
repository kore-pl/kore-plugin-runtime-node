package nodeop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNormalizeVersion 版本解析：v 前缀剥离、三段数字校验、非法值拒绝。
func TestNormalizeVersion(t *testing.T) {
	valid := map[string]string{
		"v18.19.0":   "18.19.0",
		"18.19.0":    "18.19.0",
		" v20.1.0\n": "20.1.0",
		"V22.0.0":    "22.0.0",
	}
	for raw, want := range valid {
		got, ok := normalizeVersion(raw)
		if !ok || got != want {
			t.Fatalf("normalizeVersion(%q)=(%q,%v)，期望 (%q,true)", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "v18", "v18.19", "v18.19.x", "v18..0", "node v18.19.0"} {
		if got, ok := normalizeVersion(raw); ok {
			t.Fatalf("normalizeVersion(%q) 不应接受（返回 %q）", raw, got)
		}
	}
}

// TestCompareVersions 版本比较按数字段而非字符串。
func TestCompareVersions(t *testing.T) {
	if compareVersions("18.9.0", "18.10.0") >= 0 {
		t.Fatal("18.9.0 应小于 18.10.0")
	}
	if compareVersions("20.0.0", "18.19.0") <= 0 {
		t.Fatal("20.0.0 应大于 18.19.0")
	}
	if compareVersions("18.19.0", "18.19.0") != 0 {
		t.Fatal("同版本应相等")
	}
}

// TestCountObjectKeys 顶层键计数：嵌套对象/数组里的键不计入。
func TestCountObjectKeys(t *testing.T) {
	cases := map[string]int{
		`{"a":1}`:                     1,
		`{}`:                          0,
		`null`:                        0,
		`[]`:                          0,
		`{"a":{"b":1},"c":[{"d":2}]}`: 2,
		`{"a":1,"b":2,"c":3}`:         3,
	}
	for raw, want := range cases {
		if got := countObjectKeys([]byte(raw)); got != want {
			t.Fatalf("countObjectKeys(%s)=%d，期望 %d", raw, got, want)
		}
	}
}

// TestParseNpmGlobalCount npm 全局清单计数：坏 JSON / 非对象降级为 0。
func TestParseNpmGlobalCount(t *testing.T) {
	cases := map[string]int{
		`{"dependencies":{"a":{},"b":{}}}`: 2,
		`{"dependencies":{}}`:              0,
		`{"dependencies":null}`:            0,
		`{}`:                               0,
		`not-json`:                         0,
		``:                                 0,
	}
	for raw, want := range cases {
		if got := parseNpmGlobalCount(raw); got != want {
			t.Fatalf("parseNpmGlobalCount(%q)=%d，期望 %d", raw, got, want)
		}
	}
}

// fakeLayout 构造一个包含 PATH node / nvm 软链 / 已知路径 node / npm 的临时环境。
type fakeLayout struct {
	base      string
	pathNode  string
	nvmNode   string
	knownNode string
	npmPath   string
	runner    fakeRunner
}

func newFakeLayout(t *testing.T) fakeLayout {
	t.Helper()
	base := t.TempDir()
	layout := fakeLayout{
		base:      base,
		pathNode:  filepath.Join(base, "bin", "node"),
		nvmNode:   filepath.Join(base, "home", ".nvm", "versions", "node", "v18.19.0", "bin", "node"),
		knownNode: filepath.Join(base, "opt", "node20", "bin", "node"),
		npmPath:   filepath.Join(base, "bin", "npm"),
	}
	writeFile(t, layout.pathNode, "#!/bin/sh\n")
	writeFile(t, layout.knownNode, "#!/bin/sh\n")
	writeFile(t, layout.npmPath, "#!/bin/sh\n")
	if err := os.MkdirAll(filepath.Dir(layout.nvmNode), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(layout.pathNode, layout.nvmNode); err != nil {
		t.Skipf("当前文件系统不支持符号链接: %v", err)
	}
	layout.runner = fakeRunner{
		responses: map[string]string{
			layout.pathNode + " --version":                   "v18.19.0\n",
			layout.knownNode + " --version":                  "v20.11.0\n",
			layout.npmPath + " --version":                    "9.8.1\n",
			layout.pathNode + " -p process.versions.v8":      "10.2.154.26-node.26",
			layout.pathNode + " -p process.versions.openssl": "3.0.13",
			layout.npmPath + " ls -g --depth=0 --json":       `{"dependencies":{"npm":{},"corepack":{}}}`,
			layout.npmPath + " root -g":                      filepath.Join(base, "global"),
		},
		failures: map[string]error{},
	}
	if err := os.MkdirAll(filepath.Join(base, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	return layout
}

// runtime 组装注入后的 Runtime（模拟宿主 PATH/nvm/known_path 三种来源）。
func (l fakeLayout) runtime(getenv func(string) string) *Runtime {
	return &Runtime{
		runner:   l.runner,
		lookPath: l.lookPath,
		homeDir:  func() (string, error) { return filepath.Join(l.base, "home"), nil },
		glob: func(pattern string) ([]string, error) {
			if strings.Contains(pattern, ".nvm") {
				return []string{l.nvmNode}, nil
			}
			return nil, nil
		},
		getenv:     getenv,
		knownPaths: []string{l.knownNode},
	}
}

// lookPath 模拟 PATH 解析：node/nodejs 指向同一文件，npm 指向同目录 npm。
func (l fakeLayout) lookPath(name string) (string, error) {
	switch name {
	case "node", "nodejs":
		return l.pathNode, nil
	case "npm":
		return l.npmPath, nil
	default:
		return "", errors.New("not found")
	}
}

// TestDetectDedupAndDefault 探测去重（PATH 与 nvm 软链同一文件）、默认标记与排序。
func TestDetectDedupAndDefault(t *testing.T) {
	layout := newFakeLayout(t)
	result, err := layout.runtime(os.Getenv).Versions()
	if err != nil {
		t.Fatalf("Versions 失败: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("期望 2 个去重后的安装，实际=%+v", result.Versions)
	}
	if !result.Versions[0].IsDefault || result.Versions[0].Version != "18.19.0" || result.Versions[0].Source != SourcePath {
		t.Fatalf("默认条目错误: %+v", result.Versions[0])
	}
	if result.Versions[1].Version != "20.11.0" || result.Versions[1].Source != SourceKnownPath {
		t.Fatalf("第二条目错误: %+v", result.Versions[1])
	}
	if result.DefaultVersion != "18.19.0" {
		t.Fatalf("default_version=%q", result.DefaultVersion)
	}
	defaults := 0
	for _, item := range result.Versions {
		if item.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("is_default=true 的条目不唯一: %d", defaults)
	}
}

// TestDetectSkipsBrokenCandidate 候选 --version 失败只跳过该候选。
func TestDetectSkipsBrokenCandidate(t *testing.T) {
	layout := newFakeLayout(t)
	runtime := layout.runtime(os.Getenv)
	runtime.runner = fakeRunner{failures: map[string]error{
		layout.pathNode + " --version": errors.New("exec format error"),
	}, responses: map[string]string{layout.knownNode + " --version": "v20.11.0"}}
	result, _ := runtime.Versions()
	if result.Count != 1 || result.Versions[0].Version != "20.11.0" {
		t.Fatalf("坏候选未跳过: %+v", result.Versions)
	}
	if result.Versions[0].IsDefault {
		t.Fatalf("PATH 的 node 不可用时不应有默认标记: %+v", result.Versions[0])
	}
}

// TestVersionsNoNode 完全没有 Node 时不报错：count=0 + 固定 hint。
func TestVersionsNoNode(t *testing.T) {
	runtime := &Runtime{
		runner:     fakeRunner{},
		lookPath:   func(string) (string, error) { return "", errors.New("not found") },
		homeDir:    func() (string, error) { return t.TempDir(), nil },
		glob:       func(string) ([]string, error) { return nil, nil },
		getenv:     os.Getenv,
		knownPaths: nil,
	}
	result, err := runtime.Versions()
	if err != nil {
		t.Fatalf("无 Node 时不应报错: %v", err)
	}
	if result.Count != 0 || len(result.Versions) != 0 || result.DefaultVersion != "" {
		t.Fatalf("无 Node 结果应为空: %+v", result)
	}
	if result.Hint != NoNodeHint {
		t.Fatalf("hint=%q，期望 %q", result.Hint, NoNodeHint)
	}
}

// TestInfoRealFields 详情汇总真实命令输出（假执行面）：npm/V8/OpenSSL/全局包数。
func TestInfoRealFields(t *testing.T) {
	layout := newFakeLayout(t)
	result, err := layout.runtime(os.Getenv).Info("")
	if err != nil {
		t.Fatalf("Info 失败: %v", err)
	}
	if result.Version != "18.19.0" || result.NodePath != layout.pathNode {
		t.Fatalf("版本/路径错误: %+v", result)
	}
	if result.NpmPath != layout.npmPath || result.NpmVersion != "9.8.1" {
		t.Fatalf("npm 字段错误: %+v", result)
	}
	if result.V8 != "10.2.154.26-node.26" || result.OpenSSL != "3.0.13" {
		t.Fatalf("V8/OpenSSL 错误: %+v", result)
	}
	if result.GlobalPackages != 2 {
		t.Fatalf("global_packages=%d，期望 2", result.GlobalPackages)
	}
}

// TestInfoVersionSelection 指定未安装版本返回哨兵错误，指定已安装版本命中。
func TestInfoVersionSelection(t *testing.T) {
	layout := newFakeLayout(t)
	runtime := layout.runtime(os.Getenv)
	if _, err := runtime.Info("20.11.0"); err != nil {
		t.Fatalf("已安装版本 20.11.0 应命中: %v", err)
	}
	if _, err := runtime.Info("22.0.0"); !errors.Is(err, ErrVersionNotInstalled) {
		t.Fatalf("未安装版本错误=%v，期望 ErrVersionNotInstalled", err)
	}
}

// TestInfoNoNode 完全没有 Node 时返回 ErrNodeNotInstalled。
func TestInfoNoNode(t *testing.T) {
	runtime := &Runtime{
		runner:     fakeRunner{},
		lookPath:   func(string) (string, error) { return "", errors.New("not found") },
		homeDir:    func() (string, error) { return t.TempDir(), nil },
		glob:       func(string) ([]string, error) { return nil, nil },
		getenv:     os.Getenv,
		knownPaths: nil,
	}
	if _, err := runtime.Info(""); !errors.Is(err, ErrNodeNotInstalled) {
		t.Fatalf("错误=%v，期望 ErrNodeNotInstalled", err)
	}
}

// TestDoctorChecks doctor 真实检查项：node/npm/全局目录/NODE_ENV 全部 pass，ok=true。
func TestDoctorChecks(t *testing.T) {
	layout := newFakeLayout(t)
	runtime := layout.runtime(func(key string) string {
		if key == "NODE_ENV" {
			return "production"
		}
		return ""
	})
	result, err := runtime.Doctor("")
	if err != nil {
		t.Fatalf("Doctor 失败: %v", err)
	}
	if !result.OK {
		t.Fatalf("期望 ok=true，实际 checks=%+v", result.Checks)
	}
	statuses := map[string]string{}
	for _, check := range result.Checks {
		statuses[check.Name] = check.Status
		if check.Detail == "" {
			t.Fatalf("check %s 缺少 detail", check.Name)
		}
	}
	for _, name := range []string{"node", "npm", "global_packages_writable", "node_env"} {
		if statuses[name] != StatusPass {
			t.Fatalf("check %s=%q，期望 pass（全部=%+v）", name, statuses[name], result.Checks)
		}
	}
}

// TestDoctorWarnsWithoutNodeEnv NODE_ENV 未设置是 warn，不把 ok 拉成 false。
func TestDoctorWarnsWithoutNodeEnv(t *testing.T) {
	layout := newFakeLayout(t)
	result, err := layout.runtime(func(string) string { return "" }).Doctor("")
	if err != nil {
		t.Fatalf("Doctor 失败: %v", err)
	}
	if !result.OK {
		t.Fatalf("warn 不应导致 ok=false: %+v", result.Checks)
	}
	found := false
	for _, check := range result.Checks {
		if check.Name == "node_env" {
			found = true
			if check.Status != StatusWarn {
				t.Fatalf("node_env=%q，期望 warn", check.Status)
			}
		}
	}
	if !found {
		t.Fatal("缺少 node_env 检查")
	}
}

// TestDoctorProjectRoot root 给出时追加项目目录检查。
func TestDoctorProjectRoot(t *testing.T) {
	layout := newFakeLayout(t)
	writeFile(t, filepath.Join(layout.base, "site", "package.json"), `{"name":"site"}`)
	result, err := layout.runtime(os.Getenv).Doctor(filepath.Join(layout.base, "site"))
	if err != nil {
		t.Fatalf("Doctor 失败: %v", err)
	}
	found := false
	for _, check := range result.Checks {
		if check.Name == "project_root" {
			found = true
			if check.Status != StatusPass {
				t.Fatalf("project_root=%q，期望 pass（%s）", check.Status, check.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("缺少 project_root 检查: %+v", result.Checks)
	}
}

// TestDoctorNoNode 无 Node 时 node/npm/global 都是 fail，ok=false。
func TestDoctorNoNode(t *testing.T) {
	runtime := &Runtime{
		runner:     fakeRunner{},
		lookPath:   func(string) (string, error) { return "", errors.New("not found") },
		homeDir:    func() (string, error) { return t.TempDir(), nil },
		glob:       func(string) ([]string, error) { return nil, nil },
		getenv:     os.Getenv,
		knownPaths: nil,
	}
	result, err := runtime.Doctor("")
	if err != nil {
		t.Fatalf("Doctor 不应借用错误返回: %v", err)
	}
	if result.OK {
		t.Fatalf("无 Node 时 ok 应为 false: %+v", result.Checks)
	}
	fails := 0
	for _, check := range result.Checks {
		if check.Status == StatusFail {
			fails++
		}
	}
	if fails < 2 {
		t.Fatalf("期望至少 node/npm 两个 fail，实际=%+v", result.Checks)
	}
}

// TestOSRunnerTimeout 命令执行面必须硬超时，不允许挂死。
func TestOSRunnerTimeout(t *testing.T) {
	start := time.Now()
	_, err := (osRunner{timeout: 20 * time.Millisecond}).Run("sleep", "", "5")
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("错误=%v，期望超时错误", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("超时未生效，耗时 %s", elapsed)
	}
}

// TestDirWritableAccess 目录写权限用 access(2) 真实判定（非 root 下 0555 目录不可写）。
func TestDirWritableAccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	writable, reason := dirWritable(dir)
	if os.Geteuid() != 0 && writable {
		t.Fatalf("0555 目录不应判定为可写（reason=%s）", reason)
	}
	writable, reason = dirWritable(t.TempDir())
	if !writable {
		t.Fatalf("临时目录应可写（reason=%s）", reason)
	}
}

// TestDetectInstallMetadata versions 增强字段：安装目录、包管理器与包管理器路径。
func TestDetectInstallMetadata(t *testing.T) {
	layout := newFakeLayout(t)
	result, err := layout.runtime(os.Getenv).Versions()
	if err != nil {
		t.Fatalf("Versions 失败: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("期望 2 个安装，实际 %+v", result.Versions)
	}
	pathEntry := result.Versions[0]
	if pathEntry.BinDir != filepath.Dir(layout.pathNode) {
		t.Fatalf("bin_dir=%q，期望 %q", pathEntry.BinDir, filepath.Dir(layout.pathNode))
	}
	if pathEntry.PackageManager != PackageManager {
		t.Fatalf("package_manager=%q，期望 %q", pathEntry.PackageManager, PackageManager)
	}
	if pathEntry.PackageManagerPath != layout.npmPath {
		t.Fatalf("同目录 npm 应优先：package_manager_path=%q，期望 %q", pathEntry.PackageManagerPath, layout.npmPath)
	}
	knownEntry := result.Versions[1]
	if knownEntry.BinDir != filepath.Dir(layout.knownNode) {
		t.Fatalf("已知路径条目 bin_dir=%q", knownEntry.BinDir)
	}
	if knownEntry.PackageManagerPath != layout.npmPath {
		t.Fatalf("无同目录 npm 时应回退 PATH：%q，期望 %q", knownEntry.PackageManagerPath, layout.npmPath)
	}
}

// TestPathsReportsSearchOrderAndDefault paths 方法：PATH 顺序、可执行名解析、默认解析链与来源顺序。
func TestPathsReportsSearchOrderAndDefault(t *testing.T) {
	layout := newFakeLayout(t)
	runtime := layout.runtime(func(string) string { return "/usr/local/bin:/usr/bin" })
	result, err := runtime.Paths()
	if err != nil {
		t.Fatalf("Paths 失败: %v", err)
	}
	if len(result.PathEnv) != 2 || result.PathEnv[0] != "/usr/local/bin" || result.PathEnv[1] != "/usr/bin" {
		t.Fatalf("path_env=%v", result.PathEnv)
	}
	if len(result.Executables) != 2 {
		t.Fatalf("executables=%+v", result.Executables)
	}
	for _, entry := range result.Executables {
		if !entry.Found || entry.Path != layout.pathNode {
			t.Fatalf("可执行名解析异常: %+v", entry)
		}
	}
	if !result.Default.Found || result.Default.Command != "node" {
		t.Fatalf("default 解析异常: %+v", result.Default)
	}
	if result.Default.Path != layout.pathNode || result.Default.Version != "18.19.0" {
		t.Fatalf("default=%+v", result.Default)
	}
	if result.Default.BinDir != filepath.Dir(layout.pathNode) || result.Default.PackageManagerPath != layout.npmPath {
		t.Fatalf("default 元信息异常: %+v", result.Default)
	}
	if len(result.SearchOrder) != 3 {
		t.Fatalf("search_order=%+v，期望 path/nvm/known_path 三条", result.SearchOrder)
	}
	if result.SearchOrder[0].Source != SourcePath || result.SearchOrder[0].Paths[0] != layout.pathNode {
		t.Fatalf("第一条来源异常: %+v", result.SearchOrder[0])
	}
	if result.SearchOrder[1].Source != SourceNVM || result.SearchOrder[2].Source != SourceKnownPath {
		t.Fatalf("来源顺序异常: %+v", result.SearchOrder)
	}
	// 一致性地基：candidates() 的每条路径都必须出现在 search_order 里（防止两处顺序漂移）。
	listed := map[string]bool{}
	for _, source := range result.SearchOrder {
		for _, path := range source.Paths {
			listed[path] = true
		}
	}
	for _, item := range runtime.candidates() {
		if !listed[item.path] {
			t.Fatalf("候选 %s（source=%s）未出现在 search_order", item.path, item.source)
		}
	}
}

// TestPathsWithoutNode 未安装 Node 时 paths 不报错：default.found=false + hint。
func TestPathsWithoutNode(t *testing.T) {
	runtime := &Runtime{
		runner:     fakeRunner{},
		lookPath:   func(string) (string, error) { return "", errors.New("not found") },
		homeDir:    func() (string, error) { return "", errors.New("no home") },
		glob:       func(string) ([]string, error) { return nil, nil },
		getenv:     func(string) string { return "" },
		knownPaths: nil,
	}
	result, err := runtime.Paths()
	if err != nil {
		t.Fatalf("Paths 失败: %v", err)
	}
	if result.Default.Found {
		t.Fatalf("不应解析出默认 Node: %+v", result.Default)
	}
	if result.Hint != NoNodeHint {
		t.Fatalf("hint=%q，期望 %q", result.Hint, NoNodeHint)
	}
	if len(result.PathEnv) != 0 || len(result.SearchOrder) != 0 {
		t.Fatalf("空环境结果异常: %+v", result)
	}
}
