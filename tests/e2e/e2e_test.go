// Package e2e 在本机对 runtime-node 插件做真实端到端验证：
// 真实 node/npm 探测、真实 V8/OpenSSL、真实项目扫描与健康检查，以及"未安装 Node"的降级路径。
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pengtang1024/kore-plugin-runtime-node/internal/nodeop"
	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// versionPattern 是归一化后的 Node 版本号格式。
var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// call 调用一个方法并打印真实 JSON（验收要求贴真实输出）。
func call(t *testing.T, method, params, stateRoot string) json.RawMessage {
	t.Helper()
	raw, perr := nodeop.Call(method, json.RawMessage(params), stateRoot, nil)
	if perr != nil {
		t.Fatalf("%s 失败: code=%s message=%s", method, perr.Code, perr.Message)
	}
	t.Logf("%s => %s", method, raw)
	return raw
}

// requireNode 返回本机 PATH 中的 node 路径；没有则跳过需要真实 Node 的用例。
func requireNode(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机 PATH 中没有 node，真实探测用例跳过（降级路径见 TestVersionsDegraded）")
	}
	return path
}

// TestVersionsReal 真实检测：版本格式、路径存在、source 合法、is_default 唯一且等于 which node。
func TestVersionsReal(t *testing.T) {
	nodePath := requireNode(t)
	raw := call(t, "runtime-node.versions", `{}`, "")
	var result nodeop.VersionsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if result.Count == 0 || len(result.Versions) == 0 {
		t.Fatalf("本机存在 node（%s），但检测结果为空: %s", nodePath, raw)
	}
	if result.Count != len(result.Versions) {
		t.Fatalf("count=%d 与 versions 长度 %d 不一致", result.Count, len(result.Versions))
	}
	if !versionPattern.MatchString(result.DefaultVersion) {
		t.Fatalf("default_version 格式非法: %q", result.DefaultVersion)
	}
	defaults := 0
	defaultFound := false
	for _, item := range result.Versions {
		if !versionPattern.MatchString(item.Version) {
			t.Fatalf("版本格式非法: %q", item.Version)
		}
		if _, err := os.Stat(item.Path); err != nil {
			t.Fatalf("版本路径不存在: %q (%v)", item.Path, err)
		}
		switch item.Source {
		case nodeop.SourcePath, nodeop.SourceKnownPath, nodeop.SourceNVM:
		default:
			t.Fatalf("source 非法: %q", item.Source)
		}
		if item.IsDefault {
			defaults++
			defaultFound = true
			if item.Version != result.DefaultVersion {
				t.Fatalf("默认条目版本 %q 与 default_version %q 不一致", item.Version, result.DefaultVersion)
			}
			if err := sameFile(nodePath, item.Path); err != nil {
				t.Fatalf("is_default 条目与 which node 不一致: %v", err)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("is_default=true 的条目应恰好 1 个，实际 %d", defaults)
	}
	if !defaultFound {
		t.Fatal("没有任何默认版本")
	}
	t.Logf("本机默认 Node %s（%s），共检测到 %d 个安装", result.DefaultVersion, nodePath, result.Count)
}

// TestInfoReal 详情：真实 npm 版本、V8 与 OpenSSL。
func TestInfoReal(t *testing.T) {
	requireNode(t)
	raw := call(t, "runtime-node.info", `{}`, "")
	var result nodeop.InfoResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if !versionPattern.MatchString(result.Version) || result.NodePath == "" {
		t.Fatalf("版本/路径为空: %+v", result)
	}
	if _, err := os.Stat(result.NodePath); err != nil {
		t.Fatalf("node_path 不存在: %v", err)
	}
	if result.V8 == "" {
		t.Fatalf("V8 版本为空（node -p process.versions.v8 应成功）: %+v", result)
	}
	if result.NpmPath != "" {
		if result.NpmVersion == "" {
			t.Fatalf("npm_path=%q 但 npm_version 为空: %+v", result.NpmPath, result)
		}
		if result.GlobalPackages < 0 {
			t.Fatalf("global_packages 非法: %d", result.GlobalPackages)
		}
	}
	if result.OpenSSL == "" {
		t.Logf("OpenSSL 版本为空（该 Node 构建可能未带 OpenSSL）")
	}
	t.Logf("真实详情：node=%s npm=%s v8=%s openssl=%s global_packages=%d",
		result.Version, result.NpmVersion, result.V8, result.OpenSSL, result.GlobalPackages)
}

// TestInfoVersionNotInstalled 指定未安装版本按 E_INVALID 返回，不误报。
func TestInfoVersionNotInstalled(t *testing.T) {
	requireNode(t)
	_, perr := nodeop.Call("runtime-node.info", json.RawMessage(`{"version":"0.0.1"}`), "", nil)
	if perr == nil || perr.Code != pluginapi.E_INVALID {
		t.Fatalf("错误=%v，期望 E_INVALID", perr)
	}
}

// testRoot 是验收指定的真实项目目录。
const testRoot = "/tmp/kore-node-test"

// prepareProjects 造 3 个项目：1 正常、1 坏 JSON、1 无 package.json。
func prepareProjects(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(testRoot); err != nil {
		t.Fatalf("清理 %s 失败: %v", testRoot, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(testRoot) })
	files := map[string]string{
		filepath.Join(testRoot, "good-app", "package.json"): `{
  "name": "good-app",
  "version": "1.2.3",
  "engines": {"node": ">=18"},
  "dependencies": {"express": "^4.18.0", "dotenv": "^16.4.0"}
}`,
		filepath.Join(testRoot, "good-app", "package-lock.json"): `{}`,
		filepath.Join(testRoot, "bad-json", "package.json"):      `{ "name": "broken", `,
		filepath.Join(testRoot, "no-package", "index.js"):        `console.log("no package.json")`,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
	}
}

// TestProjectsReal 真实扫描 /tmp/kore-node-test：1 个正常项目 + 坏 JSON 的 warnings。
func TestProjectsReal(t *testing.T) {
	prepareProjects(t)
	raw := call(t, "runtime-node.projects", `{"root":"`+testRoot+`"}`, testRoot)
	var result nodeop.ProjectsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if result.Count != 1 || len(result.Projects) != 1 {
		t.Fatalf("期望 1 个项目，实际 count=%d projects=%+v warnings=%v", result.Count, result.Projects, result.Warnings)
	}
	project := result.Projects[0]
	if project.Name != "good-app" || project.Version != "1.2.3" || project.EnginesNode != ">=18" {
		t.Fatalf("项目字段错误: %+v", project)
	}
	if project.DependenciesCount != 2 || !project.HasLock {
		t.Fatalf("依赖数/锁文件错误: %+v", project)
	}
	if !strings.HasSuffix(project.Path, "good-app") {
		t.Fatalf("项目路径错误: %q", project.Path)
	}
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, " | "), "bad-json") {
		t.Fatalf("坏 JSON 未产生 warnings: %v", result.Warnings)
	}
	t.Logf("扫描到 %d 个项目；warnings=%v", result.Count, result.Warnings)
}

// TestDoctorReal 健康检查返回真实检查项，ok 与 fail 项一致。
func TestDoctorReal(t *testing.T) {
	prepareProjects(t)
	raw := call(t, "runtime-node.doctor", `{"root":"`+testRoot+`"}`, testRoot)
	var result nodeop.DoctorResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if len(result.Checks) == 0 {
		t.Fatal("checks 为空")
	}
	statuses := map[string]string{}
	fails := 0
	for _, check := range result.Checks {
		if check.Name == "" || check.Detail == "" {
			t.Fatalf("检查项字段不完整: %+v", check)
		}
		switch check.Status {
		case nodeop.StatusPass, nodeop.StatusWarn:
		case nodeop.StatusFail:
			fails++
		default:
			t.Fatalf("status 非法: %+v", check)
		}
		statuses[check.Name] = check.Status
		t.Logf("check %-26s %-4s %s", check.Name, check.Status, check.Detail)
	}
	for _, name := range []string{"node", "npm", "global_packages_writable", "node_env", "project_root"} {
		if _, ok := statuses[name]; !ok {
			t.Fatalf("缺少检查项 %q: %+v", name, statuses)
		}
	}
	if result.OK != (fails == 0) {
		t.Fatalf("ok=%v 与 fail 数 %d 不一致", result.OK, fails)
	}
	if _, err := exec.LookPath("node"); err == nil && statuses["node"] != nodeop.StatusPass {
		t.Fatalf("本机有 node，但 node 检查=%q", statuses["node"])
	}
}

// TestVersionsDegraded 无 Node 时 versions 不报错：空数组 + count=0 + 固定 hint。
// 通过"空执行面"走 Call 的降级分支，验证协议层形状（真实无 Node 环境的检测降级见单测）。
func TestVersionsDegraded(t *testing.T) {
	raw, perr := nodeop.Call("runtime-node.versions", json.RawMessage(`{}`), "", emptyExecutor{})
	if perr != nil {
		t.Fatalf("无 Node 时不应报错: %v", perr)
	}
	var result nodeop.VersionsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	t.Logf("降级 JSON => %s", raw)
	if result.Count != 0 || len(result.Versions) != 0 {
		t.Fatalf("期望空版本列表: %+v", result)
	}
	if result.Hint != nodeop.NoNodeHint {
		t.Fatalf("hint=%q，期望 %q", result.Hint, nodeop.NoNodeHint)
	}
	if !strings.Contains(string(raw), `"versions":[]`) {
		t.Fatalf("versions 必须是空数组: %s", raw)
	}
}

// emptyExecutor 模拟"一个 Node 都没有"的宿主执行面（仅用于协议层降级断言）。
type emptyExecutor struct{}

func (emptyExecutor) Versions() (nodeop.VersionsResult, error) {
	return nodeop.VersionsResult{Versions: []nodeop.NodeVersion{}}, nil
}

func (emptyExecutor) Info(string) (nodeop.InfoResult, error) {
	return nodeop.InfoResult{}, nodeop.ErrNodeNotInstalled
}

func (emptyExecutor) Projects(string) (nodeop.ProjectsResult, error) {
	return nodeop.ProjectsResult{}, nil
}

func (emptyExecutor) Doctor(string) (nodeop.DoctorResult, error) {
	return nodeop.DoctorResult{Checks: []nodeop.Check{{Name: "node", Status: nodeop.StatusFail, Detail: nodeop.NoNodeHint}}}, nil
}

// sameFile 判定两个路径是否指向同一文件（跟随符号链接）。
func sameFile(left, right string) error {
	leftReal, err := filepath.EvalSymlinks(left)
	if err != nil {
		return err
	}
	rightReal, err := filepath.EvalSymlinks(right)
	if err != nil {
		return err
	}
	if leftReal != rightReal {
		return &pathMismatchError{left: leftReal, right: rightReal}
	}
	return nil
}

type pathMismatchError struct{ left, right string }

func (e *pathMismatchError) Error() string { return e.left + " != " + e.right }
