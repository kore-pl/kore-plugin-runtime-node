package nodeop

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// TestPortBoundaries 端口边界：0（省略）合法，1024/65535 合法，越界返回 E_INVALID。
func TestPortBoundaries(t *testing.T) {
	for _, port := range []int{0, 1024, 8080, 65535} {
		params, _ := json.Marshal(allocatePortParams{Preferred: port})
		result, perr := Call("runtime-node.allocatePort", params, nil)
		if perr != nil {
			t.Fatalf("端口 %d 应合法，错误=%v", port, perr)
		}
		if !strings.Contains(string(result), `"port":`+strconv.Itoa(port)) {
			t.Fatalf("端口 %d 结果=%s", port, result)
		}
	}
	for _, port := range []int{-1, 1, 1023, 65536, 100000} {
		params, _ := json.Marshal(allocatePortParams{Preferred: port})
		_, perr := Call("runtime-node.allocatePort", params, nil)
		if perr == nil || perr.Code != pluginapi.E_INVALID {
			t.Fatalf("端口 %d 应返回 E_INVALID，错误=%v", port, perr)
		}
	}
}

// TestExecPortOutOfRange 执行面返回越界端口映射为 E_INTERNAL。
func TestExecPortOutOfRange(t *testing.T) {
	badExec := executorFunc{
		versions:     func() ([]string, error) { return noopVersions(), nil },
		allocatePort: func(int) (int, error) { return 70000, nil },
	}
	params := json.RawMessage(`{"preferred":8080}`)
	_, perr := Call("runtime-node.allocatePort", params, badExec)
	if perr == nil || perr.Code != pluginapi.E_INTERNAL {
		t.Fatalf("执行面越界应返回 E_INTERNAL，错误=%v", perr)
	}
}
