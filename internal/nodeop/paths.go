package nodeop

import (
	"encoding/json"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// callPaths 处理 runtime-node.paths：无参数，返回 PATH 解析、默认 Node 解析链与发现来源顺序。
// 未检测到 Node 时不报错：default.found=false + hint 给出安装指引（与 versions 的降级口径一致）。
func callPaths(params json.RawMessage, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p pathsParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("runtime-node.paths 参数有误，请检查后重试: " + err.Error())
	}
	result, err := exec.Paths()
	if err != nil {
		return nil, classify(err)
	}
	if result.PathEnv == nil {
		result.PathEnv = []string{}
	}
	if result.Executables == nil {
		result.Executables = []PathExecutable{}
	}
	if result.SearchOrder == nil {
		result.SearchOrder = []SearchSource{}
	}
	if !result.Default.Found && result.Hint == "" {
		result.Hint = NoNodeHint
	}
	return marshal(result)
}
