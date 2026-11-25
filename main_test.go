package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

var semverPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// releaseVersions 读取 info.json / manifest.json 的 version：测试只断言「格式合法且三处同步」，
// 不锁具体版本号，bump 时无需改测试（Gitea issue 技术债）。
func releaseVersions(t *testing.T) (infoVersion, manifestVersion string) {
	t.Helper()
	read := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", path, err)
		}
		var m struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("解析 %s 失败: %v", path, err)
		}
		return m.Version
	}
	return read("info.json"), read("manifest.json")
}

// testConfig 构造握手与会话循环共用的一致配置（不依赖环境变量）。
func testConfig() config {
	return config{
		id:           "runtime-node",
		version:      "0.2.0",
		requiresKore: ">=0.1.0 <0.2.0",
		capabilities: []string{"system:admin"},
		root:         "/var/lib/kore/node",
		startedAt:    time.Now(),
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func readFrame(t *testing.T, conn net.Conn) any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	payload, err := pluginapi.ReadFrame(conn, pluginapi.MaxFrameSize)
	if err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	env, err := pluginapi.DecodeFrame(payload)
	if err != nil {
		t.Fatalf("解码帧失败: %v", err)
	}
	return env
}

func readRequest(t *testing.T, conn net.Conn) *pluginapi.Request {
	t.Helper()
	request, ok := readFrame(t, conn).(*pluginapi.Request)
	if !ok {
		t.Fatal("帧类型不是 request")
	}
	return request
}

func writeResponse(t *testing.T, conn net.Conn, id, reqID string, result json.RawMessage) {
	t.Helper()
	if err := pluginapi.WriteJSON(conn, &pluginapi.Response{
		Type: pluginapi.TypeResponse, ID: id, ReqID: reqID, Result: result,
	}); err != nil {
		t.Fatalf("写响应失败: %v", err)
	}
}

// TestHandshake 校验 hello 首帧字段与三步握手。
func TestHandshake(t *testing.T) {
	pluginConn, kernelConn := net.Pipe()
	defer func() { _ = kernelConn.Close() }()
	done := make(chan error, 1)
	go func() { done <- handshake(pluginConn, testConfig()); _ = pluginConn.Close() }()

	request := readRequest(t, kernelConn)
	if request.Method != helloMethod {
		t.Fatalf("首帧方法=%q，期望 %q", request.Method, helloMethod)
	}
	var params helloParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatalf("解析 hello 参数失败: %v", err)
	}
	if params.PluginID != "runtime-node" || params.ID != "runtime-node" {
		t.Fatalf("plugin_id/id=%q/%q，期望 runtime-node", params.PluginID, params.ID)
	}
	if params.Protocol != pluginapi.Protocol || params.Version != "0.2.0" {
		t.Fatalf("protocol/version=%q/%q 不符合 §5", params.Protocol, params.Version)
	}
	if params.PID != os.Getpid() {
		t.Fatalf("pid=%d，期望 %d", params.PID, os.Getpid())
	}
	result, err := json.Marshal(helloResult{OK: true, Protocol: pluginapi.Protocol})
	if err != nil {
		t.Fatalf("序列化握手响应失败: %v", err)
	}
	writeResponse(t, kernelConn, request.ID, request.ReqID, result)
	if err := <-done; err != nil {
		t.Fatalf("握手返回错误: %v", err)
	}
}

// TestServeShutdown 会话循环收到 kore.shutdown 后优雅退出。
// 注意：net.Pipe 无缓冲，写完 shutdown 请求必须配对读一帧响应，否则 serve 的写响应阻塞、测试死锁。
func TestServeShutdown(t *testing.T) {
	pluginConn, kernelConn := net.Pipe()
	defer func() { _ = kernelConn.Close() }()
	done := make(chan error, 1)
	go func() { done <- serve(pluginConn, testConfig(), testLogger()); _ = pluginConn.Close() }()

	request := &pluginapi.Request{
		Type: pluginapi.TypeRequest, ID: "id-1", ReqID: "r-1", Method: "kore.shutdown",
	}
	if err := pluginapi.WriteJSON(kernelConn, request); err != nil {
		t.Fatalf("写请求失败: %v", err)
	}
	// 配对读响应帧（无缓冲管道，缺这一步必然死锁）。
	env := readFrame(t, kernelConn)
	response, ok := env.(*pluginapi.Response)
	if !ok || response.ID != "id-1" {
		t.Fatalf("响应帧异常: %+v", env)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve 返回错误: %v", err)
	}
}

// TestLoadConfigEmbedded 内嵌清单的 id / protocol / capability 与任务口径一致。
func TestLoadConfigEmbedded(t *testing.T) {
	t.Setenv("KORE_PLUGIN_SOCK", "/tmp/kore-test.sock")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig 失败: %v", err)
	}
	infoVersion, manifestVersion := releaseVersions(t)
	if cfg.id != "runtime-node" {
		t.Fatalf("id=%q，期望 runtime-node", cfg.id)
	}
	if !semverPattern.MatchString(cfg.version) {
		t.Fatalf("version=%q 不是 MAJOR.MINOR.PATCH", cfg.version)
	}
	if cfg.version != infoVersion || cfg.version != manifestVersion {
		t.Fatalf("内嵌版本=%q 与 info.json=%q / manifest.json=%q 不一致", cfg.version, infoVersion, manifestVersion)
	}
	// 0.3.0 起多一项 proc:write：进程守护真的启停本机进程，方法级能力需要该域在插件级声明
	// （pluginapitest 的一致性规则：方法级只能收窄、不能跨域放大）。
	if len(cfg.capabilities) != 2 || cfg.capabilities[0] != "system:admin" || cfg.capabilities[1] != "proc:write" {
		t.Fatalf("capabilities=%v", cfg.capabilities)
	}
}
