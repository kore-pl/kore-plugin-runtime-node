package nodeop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestBusinessVerb 方法名归一化：node.<verb> 与裸 <verb> 都接受。
func TestBusinessVerb(t *testing.T) {
	cases := map[string]string{
		"node.versions": "versions", "node.allocatePort": "allocatePort",
		"versions": "versions", "allocatePort": "allocatePort",
	}
	for method, want := range cases {
		got, ok := businessVerb(method)
		if !ok || got != want {
			t.Fatalf("businessVerb(%q)=(%q,%v)，期望 (%q,true)", method, got, ok, want)
		}
	}
	if _, ok := businessVerb("node.exec"); ok {
		t.Fatal("node.exec 不应被接受")
	}
}

// TestCallResults 两方法正常路径返回结构化结果。
func TestCallResults(t *testing.T) {
	versions, perr := Call("node.versions", json.RawMessage(`{}`), nil)
	if perr != nil || !strings.Contains(string(versions), `"count":3`) {
		t.Fatalf("node.versions 结果=%s err=%v", versions, perr)
	}
	if !strings.Contains(string(versions), `"22"`) || strings.Contains(string(versions), `"24"`) {
		t.Fatalf("版本白名单错误: %s", versions)
	}
	port, perr := Call("node.allocatePort", json.RawMessage(`{"preferred":3000}`), nil)
	if perr != nil || !strings.Contains(string(port), `"port":3000`) {
		t.Fatalf("node.allocatePort 结果=%s err=%v", port, perr)
	}
	if !strings.Contains(string(port), `"allocated":false`) {
		t.Fatalf("骨架口径下 allocated 应为 false: %s", port)
	}
}

// TestCallParamValidation 非法 JSON 形状返回 E_INVALID。
func TestCallParamValidation(t *testing.T) {
	bad := []string{`[]`, `not-json`, ``, `null`, `{"family":"node"}extra`, `{"preferred":8080,"junk":1}`}
	for _, params := range bad {
		_, perr := Call("node.allocatePort", json.RawMessage(params), nil)
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("Call(allocatePort,%q) 错误=%v，期望 E_INVALID", params, perr)
		}
	}
}

// TestCallUnknownMethod 未知方法返回 E_NOTFOUND。
func TestCallUnknownMethod(t *testing.T) {
	_, perr := Call("node.exec", json.RawMessage(`{}`), nil)
	if perr == nil || perr.Code != pluginapi.E_NOTFOUND {
		t.Fatalf("未知方法错误=%v，期望 E_NOTFOUND", perr)
	}
}

// TestIsAllowedVersion 版本白名单只含 18/20/22。
func TestIsAllowedVersion(t *testing.T) {
	for _, v := range []string{"18", "20", "22"} {
		if !isAllowedVersion(v) {
			t.Fatalf("版本 %s 应在白名单", v)
		}
	}
	for _, v := range []string{"16", "21", "24", ""} {
		if isAllowedVersion(v) {
			t.Fatalf("版本 %q 不应在白名单", v)
		}
	}
}
