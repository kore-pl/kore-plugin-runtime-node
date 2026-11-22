package nodeop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// 认可的 NODE_ENV 取值。
var knownNodeEnvs = []string{"production", "development", "test"}

// callDoctor 处理 runtime-node.doctor：root 可选，给出时同样走白名单。
func callDoctor(params json.RawMessage, exec Executor, stateRoot string) (json.RawMessage, *pluginapi.Error) {
	var p doctorParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("runtime-node.doctor 参数非法: " + err.Error())
	}
	root := ""
	if p.Root != "" {
		validated, perr := ValidateRoot(p.Root, stateRoot)
		if perr != nil {
			return nil, perr
		}
		root = validated
	}
	result, err := exec.Doctor(root)
	if err != nil {
		return nil, classify(err)
	}
	if result.Checks == nil {
		result.Checks = []Check{}
	}
	result.OK = true
	for _, check := range result.Checks {
		if check.Status == StatusFail {
			result.OK = false
			break
		}
	}
	return marshal(result)
}

// Doctor 做真实健康检查：Node / npm / 全局包目录可写 / NODE_ENV / 版本冲突，
// root 非空时再检查项目目录。ok = 没有任何 fail 项。
func (r *Runtime) Doctor(root string) (DoctorResult, error) {
	versions := r.detect()
	checks := make([]Check, 0, 6)
	checks = append(checks, r.nodeCheck(versions))
	entry, hasEntry := selectVersion(versions, "")
	npmPath, npmVersion := "", ""
	if hasEntry {
		npmPath, npmVersion = r.probeNpm(entry, filepath.Dir(entry.Path))
	}
	checks = append(checks, npmCheck(npmPath, npmVersion))
	checks = append(checks, r.globalWritableCheck(entry, hasEntry, npmPath, npmVersion))
	checks = append(checks, r.nodeEnvCheck())
	if len(versions) > 1 {
		checks = append(checks, Check{
			Name:   "path_conflict",
			Status: StatusWarn,
			Detail: fmt.Sprintf("检测到 %d 个 Node 安装（%s），PATH 默认 %s；请在启动脚本里固定版本",
				len(versions), joinVersionList(versions), defaultVersionOf(versions)),
		})
	}
	if root != "" {
		checks = append(checks, r.projectRootCheck(root))
	}
	result := DoctorResult{OK: true, Checks: checks}
	for _, check := range checks {
		if check.Status == StatusFail {
			result.OK = false
			break
		}
	}
	return result, nil
}

// nodeCheck 检查是否至少有一个可用 Node。
func (r *Runtime) nodeCheck(versions []NodeVersion) Check {
	if len(versions) == 0 {
		return Check{Name: "node", Status: StatusFail, Detail: NoNodeHint}
	}
	return Check{
		Name:   "node",
		Status: StatusPass,
		Detail: fmt.Sprintf("检测到 %d 个版本，默认 %s（%s）", len(versions), defaultVersionOf(versions), defaultPathOf(versions)),
	}
}

// npmCheck 检查 npm 是否可用：路径存在且 `npm --version` 成功。
func npmCheck(npmPath, npmVersion string) Check {
	switch {
	case npmPath == "":
		return Check{Name: "npm", Status: StatusFail, Detail: "未找到 npm，Node 安装可能不完整"}
	case npmVersion == "":
		return Check{Name: "npm", Status: StatusFail, Detail: "npm 不可用（--version 执行失败）: " + npmPath}
	default:
		return Check{Name: "npm", Status: StatusPass, Detail: fmt.Sprintf("npm %s（%s）", npmVersion, npmPath)}
	}
}

// globalWritableCheck 用 `npm root -g` 定位全局包目录并检查写权限位。
func (r *Runtime) globalWritableCheck(entry NodeVersion, hasEntry bool, npmPath, npmVersion string) Check {
	name := "global_packages_writable"
	if !hasEntry {
		return Check{Name: name, Status: StatusFail, Detail: "Node 未安装，无法定位全局包目录"}
	}
	if npmPath == "" || npmVersion == "" {
		return Check{Name: name, Status: StatusFail, Detail: "npm 不可用，无法读取全局包目录"}
	}
	out, err := r.runner.Run(npmPath, filepath.Dir(entry.Path), "root", "-g")
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: "npm root -g 执行失败: " + err.Error()}
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return Check{Name: name, Status: StatusFail, Detail: "npm root -g 未返回目录"}
	}
	writable, reason := dirWritable(dir)
	if writable {
		return Check{Name: name, Status: StatusPass, Detail: fmt.Sprintf("全局包目录可写: %s（%s）", dir, reason)}
	}
	return Check{Name: name, Status: StatusWarn, Detail: fmt.Sprintf("全局包目录不可写: %s（%s）；全局安装需 sudo 或设置 npm prefix", dir, reason)}
}

// nodeEnvCheck 检查 NODE_ENV 是否设置且为标准取值。
func (r *Runtime) nodeEnvCheck() Check {
	value := strings.TrimSpace(r.getenv("NODE_ENV"))
	switch {
	case value == "":
		return Check{Name: "node_env", Status: StatusWarn, Detail: "NODE_ENV 未设置；生产环境建议 export NODE_ENV=production"}
	case containsString(knownNodeEnvs, value):
		return Check{Name: "node_env", Status: StatusPass, Detail: "NODE_ENV=" + value}
	default:
		return Check{Name: "node_env", Status: StatusWarn, Detail: "NODE_ENV 非标准取值: " + value + "（建议 production/development/test）"}
	}
}

// projectRootCheck 检查指定 root 下是否能发现 Node 项目。
func (r *Runtime) projectRootCheck(root string) Check {
	result, err := r.Projects(root)
	if err != nil {
		return Check{Name: "project_root", Status: StatusFail, Detail: "项目目录扫描失败: " + err.Error()}
	}
	if result.Count == 0 {
		return Check{Name: "project_root", Status: StatusWarn, Detail: "目录下未发现 package.json: " + root}
	}
	return Check{Name: "project_root", Status: StatusPass, Detail: fmt.Sprintf("目录下发现 %d 个 Node 项目: %s", result.Count, root)}
}

// dirWritable 判定当前进程是否可写目录：优先 access(2) 真实探测，
// 不支持该调用的平台退回权限位启发式，并返回人类可读依据。
func dirWritable(path string) (bool, string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, "目录不存在或不可访问"
	}
	if !info.IsDir() {
		return false, "不是目录"
	}
	if writable, supported := probeDirWritable(path); supported {
		if writable {
			return true, "access(W_OK)=可写"
		}
		return false, "access(W_OK)=不可写"
	}
	mode := info.Mode().Perm()
	if os.Geteuid() == 0 {
		return true, fmt.Sprintf("mode=%04o, uid=0", mode)
	}
	if mode&0o222 != 0 {
		return true, fmt.Sprintf("mode=%04o（权限位启发式）", mode)
	}
	return false, fmt.Sprintf("mode=%04o 无写权限位", mode)
}

// defaultVersionOf 返回默认版本号；没有默认时退回第一个。
func defaultVersionOf(versions []NodeVersion) string {
	if entry, ok := selectVersion(versions, ""); ok {
		return entry.Version
	}
	return ""
}

// defaultPathOf 返回默认版本路径。
func defaultPathOf(versions []NodeVersion) string {
	if entry, ok := selectVersion(versions, ""); ok {
		return entry.Path
	}
	return ""
}

// containsString 判断切片是否包含给定值。
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
