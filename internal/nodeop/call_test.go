package nodeop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestBusinessVerb 方法名归一化：只接受 5 个 runtime-node.<verb>，域不符或裸动词一律拒绝。
func TestBusinessVerb(t *testing.T) {
	cases := map[string]string{
		"runtime-node.versions": "versions",
		"runtime-node.info":     "info",
		"runtime-node.projects": "projects",
		"runtime-node.doctor":   "doctor",
		"runtime-node.paths":    "paths",
	}
	for method, want := range cases {
		got, ok := businessVerb(method)
		if !ok || got != want {
			t.Fatalf("businessVerb(%q)=(%q,%v)，期望 (%q,true)", method, got, ok, want)
		}
	}
	for _, method := range []string{
		"runtime-node.allocatePort", "node.versions", "versions", "runtime-node.info2", "runtime-node.",
	} {
		if _, ok := businessVerb(method); ok {
			t.Fatalf("businessVerb(%q) 不应被接受", method)
		}
	}
}

// TestCallVersionsNoNode 未检测到 Node 时 versions 不报错：空数组 + count=0 + 固定 hint。
func TestCallVersionsNoNode(t *testing.T) {
	exec := &stubExecutor{versions: VersionsResult{}}
	raw, perr := Call("runtime-node.versions", json.RawMessage(`{}`), "", exec)
	if perr != nil {
		t.Fatalf("未安装 Node 时不应报错: %v", perr)
	}
	var result VersionsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if result.Count != 0 || len(result.Versions) != 0 {
		t.Fatalf("期望空版本列表: %+v", result)
	}
	if result.Hint != NoNodeHint {
		t.Fatalf("hint=%q，期望 %q", result.Hint, NoNodeHint)
	}
	if !strings.Contains(string(raw), `"versions":[]`) {
		t.Fatalf("versions 必须是空数组而非 null: %s", raw)
	}
}

// TestCallVersionsResult 已安装时返回版本列表、默认版本与计数。
func TestCallVersionsResult(t *testing.T) {
	exec := &stubExecutor{versions: VersionsResult{
		Versions: []NodeVersion{
			{Version: "18.19.0", Path: "/usr/bin/node", Source: SourcePath, IsDefault: true},
		},
		DefaultVersion: "18.19.0",
	}}
	raw, perr := Call("runtime-node.versions", json.RawMessage(`{}`), "", exec)
	if perr != nil {
		t.Fatalf("versions 意外错误: %v", perr)
	}
	if !strings.Contains(string(raw), `"default_version":"18.19.0"`) || !strings.Contains(string(raw), `"count":1`) {
		t.Fatalf("结果=%s", raw)
	}
}

// TestCallInfoNormalizesVersion 输入 v18.19.0 归一为 18.19.0 后交给执行面。
func TestCallInfoNormalizesVersion(t *testing.T) {
	exec := &stubExecutor{info: InfoResult{Version: "18.19.0"}}
	if _, perr := Call("runtime-node.info", json.RawMessage(`{"version":"v18.19.0"}`), "", exec); perr != nil {
		t.Fatalf("info 意外错误: %v", perr)
	}
	if exec.infoArg != "18.19.0" {
		t.Fatalf("执行面收到版本=%q，期望 18.19.0", exec.infoArg)
	}
}

// TestCallErrorMapping 执行面错误按协议映射：未装 Node → E_NOTFOUND，版本未装 → E_INVALID。
func TestCallErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"node_missing", ErrNodeNotInstalled, pluginapi.E_NOTFOUND},
		{"version_missing", wrapVersionError("20.0.0"), pluginapi.E_INVALID},
		{"kill", NewFaultError(FaultKill, "被终止"), pluginapi.E_UPSTREAM},
		{"timeout", NewFaultError(FaultTimeout, "超时"), pluginapi.E_UPSTREAM},
		{"disk", NewFaultError(FaultDisk, "磁盘满"), pluginapi.E_INTERNAL},
		{"cgroup", NewFaultError(FaultCgroup, "限额"), pluginapi.E_UNSUPPORTED},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exec := &stubExecutor{infoErr: c.err}
			_, perr := Call("runtime-node.info", json.RawMessage(`{}`), "", exec)
			if perr == nil || perr.Code != c.code {
				t.Fatalf("错误=%v，期望 %q", perr, c.code)
			}
		})
	}
}

// wrapVersionError 构造与生产实现同形的「指定版本未安装」错误。
func wrapVersionError(version string) error {
	return &versionError{version: version}
}

type versionError struct{ version string }

func (e *versionError) Error() string { return ErrVersionNotInstalled.Error() + ": " + e.version }

func (e *versionError) Is(target error) bool { return target == ErrVersionNotInstalled }

// TestCallParamValidation 非法 JSON 形状、未知字段、非法版本与越权 root 均返回 E_INVALID。
func TestCallParamValidation(t *testing.T) {
	stateRoot := t.TempDir()
	cases := []struct {
		method string
		params string
	}{
		{"runtime-node.versions", `[]`},
		{"runtime-node.versions", `null`},
		{"runtime-node.versions", `{"family":"node"}`},
		{"runtime-node.info", `{"version":18}`},
		{"runtime-node.info", `{"version":"18.19"}`},
		{"runtime-node.info", `{"version":"18.19.0","junk":1}`},
		{"runtime-node.projects", `{}`},
		{"runtime-node.projects", `{"root":"relative/path"}`},
		{"runtime-node.projects", `{"root":"/etc"}`},
		{"runtime-node.projects", `{"root":"/var/www/../etc"}`},
		{"runtime-node.doctor", `{"root":"/etc"}`},
	}
	for _, c := range cases {
		_, perr := Call(c.method, json.RawMessage(c.params), stateRoot, &stubExecutor{})
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("Call(%s,%s) 错误=%v，期望 E_INVALID", c.method, c.params, perr)
		}
	}
}

// TestCallProjectsPassesValidatedRoot 合法 root 经白名单解析后传给执行面。
func TestCallProjectsPassesValidatedRoot(t *testing.T) {
	stateRoot := t.TempDir()
	exec := &stubExecutor{projects: ProjectsResult{}}
	raw, perr := Call("runtime-node.projects", json.RawMessage(`{"root":"`+stateRoot+`"}`), stateRoot, exec)
	if perr != nil {
		t.Fatalf("projects 意外错误: %v", perr)
	}
	if exec.projectsArg == "" {
		t.Fatal("执行面未收到 root")
	}
	if !strings.Contains(string(raw), `"projects":[]`) || !strings.Contains(string(raw), `"warnings":[]`) {
		t.Fatalf("结果应为空数组而非 null: %s", raw)
	}
}

// TestCallDoctorOptionalRoot doctor 的 root 可省略（不触发项目检查）。
func TestCallDoctorOptionalRoot(t *testing.T) {
	exec := &stubExecutor{doctor: DoctorResult{Checks: []Check{{Name: "node", Status: StatusPass, Detail: "ok"}}}}
	raw, perr := Call("runtime-node.doctor", json.RawMessage(`{}`), t.TempDir(), exec)
	if perr != nil {
		t.Fatalf("doctor 意外错误: %v", perr)
	}
	if exec.doctorArg != "" {
		t.Fatalf("未给 root 时执行面应收空串，实际=%q", exec.doctorArg)
	}
	if !strings.Contains(string(raw), `"ok":true`) {
		t.Fatalf("结果=%s", raw)
	}
}

// TestCallUnknownMethod 未知方法返回 E_NOTFOUND。
func TestCallUnknownMethod(t *testing.T) {
	for _, method := range []string{"runtime-node.exec", "runtime-node.allocatePort", "versions"} {
		_, perr := Call(method, json.RawMessage(`{}`), "", &stubExecutor{})
		if perr == nil || perr.Code != pluginapi.E_NOTFOUND {
			t.Fatalf("Call(%q) 错误=%v，期望 E_NOTFOUND", method, perr)
		}
	}
}

// TestCallPathsNormalizes 空结果归一：数组字段不为 null，未找到默认 Node 时补 hint。
func TestCallPathsNormalizes(t *testing.T) {
	exec := &stubExecutor{paths: PathsResult{}}
	raw, perr := Call("runtime-node.paths", json.RawMessage(`{}`), "", exec)
	if perr != nil {
		t.Fatalf("paths 意外错误: %v", perr)
	}
	text := string(raw)
	for _, want := range []string{`"path_env":[]`, `"executables":[]`, `"search_order":[]`, `"hint":"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("结果=%s，缺少 %q", text, want)
		}
	}
}

// TestCallPathsRejectsBadParams 参数必须是 JSON 对象、拒绝未知字段（strictUnmarshal 口径）。
func TestCallPathsRejectsBadParams(t *testing.T) {
	for _, params := range []string{`[]`, `null`, ``, `{"verbose":true}`} {
		_, perr := Call("runtime-node.paths", json.RawMessage(params), "", &stubExecutor{})
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("params=%q 错误=%v，期望 E_INVALID", params, perr)
		}
	}
}

// TestCallPathsPropagatesFault 执行面故障映射为协议错误码（paths 与其余方法同一口径）。
func TestCallPathsPropagatesFault(t *testing.T) {
	exec := &stubExecutor{pathsErr: NewFaultError(FaultTimeout, "paths 超时")}
	_, perr := Call("runtime-node.paths", json.RawMessage(`{}`), "", exec)
	if perr == nil || perr.Code != pluginapi.E_UPSTREAM {
		t.Fatalf("错误=%v，期望 E_UPSTREAM", perr)
	}
}
