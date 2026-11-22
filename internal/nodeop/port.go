package nodeop

import (
	"encoding/json"
	"fmt"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// callAllocatePort 校验端口范围并返回结构化结果（不真分配、不 bind 端口）。
// preferred 省略（0）时表示由调度侧分配；给出时必须在 1024-65535 内。
func callAllocatePort(params json.RawMessage, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p allocatePortParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("node.allocatePort 参数非法: " + err.Error())
	}
	if p.Preferred != 0 && (p.Preferred < minPort || p.Preferred > maxPort) {
		return nil, invalid(fmt.Sprintf("node.allocatePort 端口 %d 越界，允许 %d-%d", p.Preferred, minPort, maxPort))
	}
	chosen, err := exec.AllocatePort(p.Preferred)
	if err != nil {
		return nil, classify(err)
	}
	if chosen != 0 && (chosen < minPort || chosen > maxPort) {
		return nil, internalErr(fmt.Sprintf("执行面返回端口 %d 越界", chosen))
	}
	result := AllocatePortResult{
		Port:      chosen,
		Allocated: false, // 骨架口径：从不真占用端口
		Applied:   false,
	}
	return marshal(result)
}

// noopAllocatePort 回显请求端口（0 表示未指定），不 bind 任何 socket。
func noopAllocatePort(preferred int) int { return preferred }
