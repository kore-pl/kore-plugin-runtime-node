package nodeop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// extFake 在 stubExecutor 之上实现 ExtManager，记录调用参数并返回预设结果。
type extFake struct {
	*stubExecutor
	installPrefix string
	installPkgs   []string
	installResult ExtInstallResult
	installErr    error
	installCalls  int

	listResult ExtListResult
	listErr    error

	removePrefix string
	removePkgs   []string
	removeResult ExtRemoveResult
	removeErr    error
	removeCalls  int
}

func (f *extFake) InstallPackages(prefix string, packages []string) (ExtInstallResult, error) {
	f.installCalls++
	f.installPrefix, f.installPkgs = prefix, packages
	return f.installResult, f.installErr
}

func (f *extFake) ListPackages(prefix string) (ExtListResult, error) {
	return f.listResult, f.listErr
}

func (f *extFake) RemovePackages(prefix string, packages []string) (ExtRemoveResult, error) {
	f.removeCalls++
	f.removePrefix, f.removePkgs = prefix, packages
	return f.removeResult, f.removeErr
}

func mustCallExt[T any](t *testing.T, method, params, root string, exec Executor) T {
	t.Helper()
	raw, perr := Call(method, json.RawMessage(params), root, exec)
	if perr != nil {
		t.Fatalf("%s 失败: %+v", method, perr)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s 结果解析失败: %v（%s）", method, err, raw)
	}
	return out
}

// TestExtInstallPrefixIsolated 校验 install/list/remove 的 prefix 固定为 <state>/global，
// 且与 /var/lib/kore、宿主 node_modules 无关（不写系统全局）。
func TestExtInstallPrefixIsolated(t *testing.T) {
	root := t.TempDir()
	fake := &extFake{}
	want := filepath.Join(root, DefaultGlobalSubdir)
	result := mustCallExt[ExtInstallResult](t, "runtime-node.ext.install",
		`{"packages":["chalk==5.0.0"]}`, root, fake)
	if result.Prefix != want || fake.installPrefix != want {
		t.Fatalf("隔离前缀不对: result=%s fake=%s want=%s", result.Prefix, fake.installPrefix, want)
	}
	if !result.NetworkUsed || len(result.Packages) != 1 || result.Packages[0] != "chalk==5.0.0" {
		t.Fatalf("安装结果字段不对: %+v", result)
	}
	list := mustCallExt[ExtListResult](t, "runtime-node.ext.list", `{}`, root, fake)
	if list.Prefix != want {
		t.Fatalf("list 前缀不对: %s", list.Prefix)
	}
	_ = mustCallExt[ExtRemoveResult](t, "runtime-node.ext.remove",
		`{"packages":["chalk"]}`, root, fake)
	if fake.removePrefix != want {
		t.Fatalf("remove 前缀不对: %s", fake.removePrefix)
	}
	if strings.HasSuffix(want, "node_modules") {
		t.Fatalf("前缀应是隔离根 <state>/global，而非 node_modules 字面路径: %s", want)
	}
}

// TestExtRejectsGlobalAndInjection 校验包名白名单挡 -g / 注入 / scope / 空列表。
func TestExtRejectsGlobalAndInjection(t *testing.T) {
	root := t.TempDir()
	fake := &extFake{}
	bad := []string{
		"-g", "--global", "npm -g", "chalk; rm -rf /", "chalk extra",
		"@scope/pkg", "pkg$(id)", "pkg`whoami`", "../evil", "/etc/passwd", "",
	}
	for _, pkg := range bad {
		payload, _ := json.Marshal(map[string]any{"packages": []string{pkg}})
		_, perr := Call("runtime-node.ext.install", payload, root, fake)
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("包名 %q 期望 E_INVALID，得到 %+v", pkg, perr)
		}
	}
	_, perr := Call("runtime-node.ext.install", json.RawMessage(`{}`), root, fake)
	if perr == nil || perr.Code != pluginapi.E_INVALID {
		t.Fatalf("空 packages 期望 E_INVALID，得到 %+v", perr)
	}
	if fake.installCalls != 0 {
		t.Fatalf("参数校验失败仍触达执行面: %d", fake.installCalls)
	}
	// 未知字段（如 prefix / global）也必须拒绝，防止调用方绕过固定前缀。
	for _, params := range []string{`{"packages":["chalk"],"prefix":"/etc"}`, `{"packages":["chalk"],"global":true}`} {
		_, perr = Call("runtime-node.ext.install", json.RawMessage(params), root, fake)
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("未知字段参数 %s 期望 E_INVALID，得到 %+v", params, perr)
		}
	}
}

// TestExtPrefixRejectsEscape 校验状态目录含 .. / 相对路径 / 首尾空白时被拒。
func TestExtPrefixRejectsEscape(t *testing.T) {
	fake := &extFake{}
	for _, root := range []string{"/tmp/../etc", "relative/state", " /tmp/state", "/tmp/state ", ""} {
		_, perr := Call("runtime-node.ext.list", json.RawMessage(`{}`), root, fake)
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("stateRoot=%q 期望 E_INVALID，得到 %+v", root, perr)
		}
	}
	// <state>/global 被软链指向状态目录之外时，真实层校验必须拒绝（软链逃逸）。
	state := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(state, DefaultGlobalSubdir)); err != nil {
		t.Skipf("本环境不支持符号链接: %v", err)
	}
	_, perr := Call("runtime-node.ext.list", json.RawMessage(`{}`), state, fake)
	if perr == nil || perr.Code != pluginapi.E_INVALID {
		t.Fatalf("global 软链逃逸期望 E_INVALID，得到 %+v", perr)
	}
}

// TestExtTruncateOutput 校验输出超限时保留首尾与标记。
func TestExtTruncateOutput(t *testing.T) {
	raw := strings.Repeat("a", MaxNpmOutputBytes+100)
	truncated, ok := truncateExtOutput(raw)
	if !ok || !strings.Contains(truncated, "输出截断") || len(truncated) >= len(raw) {
		t.Fatalf("截断逻辑异常: ok=%v len=%d", ok, len(truncated))
	}
	if ok := extPackageSpecPattern.MatchString("--global"); ok {
		t.Fatal("-g 选项不应匹配包名白名单")
	}
}

// TestExtRealManagerArgv 校验真实执行面固定 argv：绝不出现 -g/--global，prefix 指向隔离目录。
func TestExtRealManagerArgv(t *testing.T) {
	var captured []string
	mgr := realExtManager{runner: func(timeout time.Duration, name string, args ...string) (string, error) {
		if name != "npm" || timeout != ExtCommandTimeout {
			t.Fatalf("固定 argv 异常: name=%s timeout=%s", name, timeout)
		}
		captured = append([]string{}, args...)
		return "ok", nil
	}}
	prefix := filepath.Join(t.TempDir(), DefaultGlobalSubdir)
	if _, err := mgr.InstallPackages(prefix, []string{"chalk", "commander==12.0.0"}); err != nil {
		t.Fatalf("install 失败: %v", err)
	}
	joined := strings.Join(captured, " ")
	if strings.Contains(joined, "-g") || strings.Contains(joined, "--global") {
		t.Fatalf("真实执行面出现了全局安装选项: %s", joined)
	}
	if !strings.HasPrefix(joined, "install --prefix "+prefix+" ") {
		t.Fatalf("未使用固定 --prefix 隔离前缀: %s", joined)
	}
	if captured[len(captured)-2] != "chalk" || captured[len(captured)-1] != "commander==12.0.0" {
		t.Fatalf("包名未按白名单透传: %v", captured)
	}
	captured = nil
	if _, err := mgr.RemovePackages(prefix, []string{"chalk"}); err != nil {
		t.Fatalf("remove 失败: %v", err)
	}
	if strings.Contains(strings.Join(captured, " "), "-g") || captured[0] != "uninstall" {
		t.Fatalf("remove argv 异常: %v", captured)
	}
}

// TestExtClassifyTimeout 校验超时映射为 E_UPSTREAM（classify 的 FaultTimeout 分支）。
func TestExtClassifyTimeout(t *testing.T) {
	fake := &extFake{installErr: ErrExtTimeout}
	_, perr := Call("runtime-node.ext.install", json.RawMessage(`{"packages":["chalk"]}`), t.TempDir(), fake)
	if perr == nil || perr.Code != pluginapi.E_UPSTREAM {
		t.Fatalf("超时期望 E_UPSTREAM，得到 %+v", perr)
	}
}
