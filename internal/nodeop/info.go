package nodeop

import (
	"encoding/json"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// callInfo 处理 runtime-node.info：version 可省略（取默认版本），给出时必须已安装。
func callInfo(params json.RawMessage, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p infoParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("runtime-node.info 参数非法: " + err.Error())
	}
	if p.Version != "" {
		normalized, ok := normalizeVersion(p.Version)
		if !ok {
			return nil, invalid("runtime-node.info version 格式非法（期望 X.Y.Z）: " + p.Version)
		}
		p.Version = normalized
	}
	result, err := exec.Info(p.Version)
	if err != nil {
		return nil, classify(err)
	}
	return marshal(result)
}
