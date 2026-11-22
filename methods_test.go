package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestDispatchVersions 版本检测经 dispatch 返回真实探测结果（本机无 Node 时 count=0 + hint）。
func TestDispatchVersions(t *testing.T) {
	req := &pluginapi.Request{
		Type: pluginapi.TypeRequest, ID: "id", ReqID: "r",
		Method: "runtime-node.versions", Params: json.RawMessage(`{}`),
	}
	resp, shutdown := dispatch(req, testConfig(), testLogger())
	if shutdown {
		t.Fatal("versions 不应触发 shutdown")
	}
	if resp.Error != nil {
		t.Fatalf("versions 意外错误: %v", resp.Error)
	}
	var result struct {
		Count int    `json:"count"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("解析结果失败: %v（%s）", err, resp.Result)
	}
	if result.Count == 0 && result.Hint != "未检测到 Node.js，可从 https://nodejs.org 安装或用 nvm" {
		t.Fatalf("count=0 时 hint 不匹配: %s", resp.Result)
	}
}

// TestDispatchInfo 详情经 dispatch 返回；本机无 Node 时按 E_NOTFOUND 降级。
func TestDispatchInfo(t *testing.T) {
	req := &pluginapi.Request{
		Type: pluginapi.TypeRequest, ID: "id", ReqID: "r",
		Method: "runtime-node.info", Params: json.RawMessage(`{}`),
	}
	resp, _ := dispatch(req, testConfig(), testLogger())
	if resp.Error != nil {
		if resp.Error.Code != pluginapi.E_NOTFOUND {
			t.Fatalf("info 错误码=%q，期望 E_NOTFOUND 或成功", resp.Error.Code)
		}
		return
	}
	if !strings.Contains(string(resp.Result), `"node_path"`) {
		t.Fatalf("info 结果缺少 node_path: %s", resp.Result)
	}
}

// TestDispatchDoctor doctor 经 dispatch 返回检查项列表。
func TestDispatchDoctor(t *testing.T) {
	req := &pluginapi.Request{
		Type: pluginapi.TypeRequest, ID: "id", ReqID: "r",
		Method: "runtime-node.doctor", Params: json.RawMessage(`{}`),
	}
	resp, _ := dispatch(req, testConfig(), testLogger())
	if resp.Error != nil {
		t.Fatalf("doctor 意外错误: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `"checks":[`) || !strings.Contains(string(resp.Result), `"status"`) {
		t.Fatalf("doctor 结果缺少检查项: %s", resp.Result)
	}
}

// TestDispatchReject 非法参数与越权 root 经 dispatch 返回协议错误。
func TestDispatchReject(t *testing.T) {
	cases := []struct {
		method string
		params string
		code   string
	}{
		{"runtime-node.versions", `{"family":"node"}`, pluginapi.E_INVALID},
		{"runtime-node.info", `{"version":"18.19"}`, pluginapi.E_INVALID},
		{"runtime-node.projects", `{}`, pluginapi.E_INVALID},
		{"runtime-node.projects", `{"root":"/etc"}`, pluginapi.E_INVALID},
		{"runtime-node.doctor", `{"root":"/etc"}`, pluginapi.E_INVALID},
		{"runtime-node.allocatePort", `{"preferred":8080}`, pluginapi.E_NOTFOUND},
		{"runtime-node.exec", `{}`, pluginapi.E_NOTFOUND},
	}
	for _, c := range cases {
		req := &pluginapi.Request{
			Type: pluginapi.TypeRequest, ID: "id", ReqID: "r",
			Method: c.method, Params: json.RawMessage(c.params),
		}
		resp, _ := dispatch(req, testConfig(), testLogger())
		if resp.Error == nil || resp.Error.Code != c.code {
			t.Fatalf("%s 错误=%v，期望 %q", c.method, resp.Error, c.code)
		}
		if resp.Result != nil {
			t.Fatalf("%s 出错时不应带结果", c.method)
		}
	}
}

// TestPingResult kore.ping 返回 ready 状态。
func TestPingResult(t *testing.T) {
	req := &pluginapi.Request{Type: pluginapi.TypeRequest, ID: "id", ReqID: "r", Method: "kore.ping"}
	resp, _ := dispatch(req, testConfig(), testLogger())
	if !strings.Contains(string(resp.Result), `"status":"ready"`) {
		t.Fatalf("ping 结果=%s", resp.Result)
	}
}
