package nodeop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// lockFiles 是判定 has_lock 的锁文件集合（npm / npm shrinkwrap / yarn / pnpm）。
var lockFiles = []string{"package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml"}

// callProjects 处理 runtime-node.projects：root 必填且必须在白名单内。
func callProjects(params json.RawMessage, exec Executor, stateRoot string) (json.RawMessage, *pluginapi.Error) {
	var p projectsParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("runtime-node.projects 参数有误，请检查后重试: " + err.Error())
	}
	root, perr := ValidateRoot(p.Root, stateRoot)
	if perr != nil {
		return nil, perr
	}
	result, err := exec.Projects(root)
	if err != nil {
		return nil, classify(err)
	}
	if result.Projects == nil {
		result.Projects = []Project{}
	}
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	result.Count = len(result.Projects)
	return marshal(result)
}

// Projects 扫描 root 下深度 ≤3 的 Node 项目（含 root 自身的 package.json）。
// 条目数上限 200；package.json 只读前 64 KiB；坏 JSON / 超大文件跳过并写 warnings。
// 不跟随目录符号链接，不执行项目脚本，只读元数据。
func (r *Runtime) Projects(root string) (ProjectsResult, error) {
	result := ProjectsResult{Projects: []Project{}, Warnings: []string{}}
	scanned := 0
	truncated := false
	var walk func(dir string, depth int) bool
	walk = func(dir string, depth int) bool {
		result.Projects = r.appendProject(dir, &result)
		if depth >= MaxScanDepth {
			return true
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			addWarning(&result.Warnings, "目录不可读，已跳过: %s", dir)
			return true
		}
		for _, entry := range entries {
			if scanned >= MaxScanEntries {
				truncated = true
				return false
			}
			scanned++
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == "node_modules" || name == ".git" || strings.HasPrefix(name, ".") {
				continue
			}
			if !walk(filepath.Join(dir, name), depth+1) {
				return false
			}
		}
		return true
	}
	walk(root, 0)
	if truncated {
		addWarning(&result.Warnings, "扫描条目达到上限 %d，结果可能不完整", MaxScanEntries)
	}
	sort.Slice(result.Projects, func(i, j int) bool { return result.Projects[i].Path < result.Projects[j].Path })
	result.Count = len(result.Projects)
	return result, nil
}

// appendProject 读取 dir/package.json 并追加项目；缺失不是项目，坏文件写 warning 后跳过。
func (r *Runtime) appendProject(dir string, result *ProjectsResult) []Project {
	pkgPath := filepath.Join(dir, "package.json")
	info, err := os.Lstat(pkgPath)
	if err != nil || !info.Mode().IsRegular() {
		return result.Projects
	}
	if info.Size() > MaxPackageJSONBytes {
		addWarning(&result.Warnings, "package.json 超过 %d 字节，已跳过: %s", MaxPackageJSONBytes, pkgPath)
		return result.Projects
	}
	data, err := readLimited(pkgPath)
	if err != nil {
		addWarning(&result.Warnings, "读取 package.json 失败，已跳过: %s", pkgPath)
		return result.Projects
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		addWarning(&result.Warnings, "package.json 不是 JSON 对象，已跳过: %s", pkgPath)
		return result.Projects
	}
	var manifest packageManifest
	if err := json.Unmarshal(trimmed, &manifest); err != nil {
		addWarning(&result.Warnings, "package.json 解析失败，已跳过: %s", pkgPath)
		return result.Projects
	}
	return append(result.Projects, Project{
		Path:              dir,
		Name:              manifest.Name,
		Version:           manifest.Version,
		EnginesNode:       manifest.Engines.Node,
		DependenciesCount: countObjectKeys(manifest.Dependencies),
		HasLock:           hasLockFile(dir),
	})
}

// readLimited 只读取文件前 MaxPackageJSONBytes 字节（防巨大文件耗内存）。
func readLimited(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, MaxPackageJSONBytes))
}

// hasLockFile 判断项目目录下是否存在任一锁文件。
func hasLockFile(dir string) bool {
	for _, name := range lockFiles {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// addWarning 追加一条告警；相同内容只保留一次。
func addWarning(warnings *[]string, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	for _, existing := range *warnings {
		if existing == message {
			return
		}
	}
	*warnings = append(*warnings, message)
}
