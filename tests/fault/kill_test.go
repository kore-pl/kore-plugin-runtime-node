package fault

import (
	"encoding/json"
	"testing"

	"github.com/pengtang1024/kore-plugin-runtime-node/internal/nodeop"
)

// TestKillFault 注入 kill -9（进程被信号终止），期望四方法返回 E_UPSTREAM。
func TestKillFault(t *testing.T) {
	assertFault(t, nodeop.FaultKill, "E_UPSTREAM", "信号 9")
}

// TestKillFaultContract 单独验证 kill 故障的错误体结构完整（真实调用，非空壳）。
func TestKillFaultContract(t *testing.T) {
	root := t.TempDir()
	_, perr := nodeop.Call("runtime-node.versions", json.RawMessage(`{}`), root, faultExecutor{kind: nodeop.FaultKill})
	verifyErrorContract(t, perr)
}
