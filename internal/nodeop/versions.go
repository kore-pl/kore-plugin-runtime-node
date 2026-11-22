package nodeop

import (
	"encoding/json"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// callVersions 校验参数并返回受支持的 Node 版本白名单（不真查询宿主 node）。
// family 可省略；给出时只接受 "node"，其余返回 E_INVALID。
func callVersions(params json.RawMessage, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p versionsParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("runtime-node.versions 参数非法: " + err.Error())
	}
	if p.Family != "" && p.Family != "node" {
		return nil, invalid("runtime-node.versions family 只接受 node，收到: " + p.Family)
	}
	versions, err := exec.Versions()
	if err != nil {
		return nil, classify(err)
	}
	result := VersionsResult{Versions: versions, Count: len(versions), Applied: false}
	return marshal(result)
}

// noopVersions 返回白名单副本，不读宿主环境（骨架口径）。
func noopVersions() []string {
	out := make([]string, len(allowedVersions))
	copy(out, allowedVersions)
	return out
}

// isAllowedVersion 判断版本是否在白名单内（供测试与后续生产实现复用）。
func isAllowedVersion(v string) bool {
	for _, allowed := range allowedVersions {
		if v == allowed {
			return true
		}
	}
	return false
}
