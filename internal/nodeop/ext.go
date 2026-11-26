package nodeop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// 扩展管理的能力边界：只在插件状态目录的隔离前缀（<state>/global）内安装/列出/移除 npm 包。
// 绝不调用 npm -g、不写宿主 node_modules、不改动任何系统目录；固定 argv、固定超时、输出截断。
const (
	// DefaultGlobalSubdir 是隔离前缀相对插件状态目录的固定子目录名。
	DefaultGlobalSubdir = "global"
	// ExtCommandTimeout 是单条 npm 命令的硬超时（联网安装较慢）。
	ExtCommandTimeout = 120 * time.Second
	// MaxExtPackages 是单次安装/移除的包数量上限。
	MaxExtPackages = 50
	// MaxNpmOutputBytes 是 npm 输出的保留上限，超出后保留首尾并标记 truncated。
	MaxNpmOutputBytes = 64 * 1024
)

// extPackageSpecPattern 是包名白名单：字母数字开头，其余允许 . _ -，可选 ==version。
// 空格 / 分号 / 引号 / 斜杠 / 前导 -（选项注入）/ npm scope 一律不匹配——
// 白名单先于 argv 拼接，保证用户参数永远无法变成 npm 选项或路径穿越。
var extPackageSpecPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}(==[A-Za-z0-9][A-Za-z0-9._-]{0,63})?$`)

var (
	// ErrExtTimeout 表示 npm 命令超时。
	ErrExtTimeout = errors.New("扩展命令超时")
	// ErrExtPrefixMissing 表示隔离前缀目录不存在（未安装过任何扩展）。
	ErrExtPrefixMissing = errors.New("隔离扩展目录不存在")
)

// ExtInstallResult 是 runtime-node.ext.install 的结果。
type ExtInstallResult struct {
	Prefix      string   `json:"prefix"`
	Packages    []string `json:"packages"`
	Output      string   `json:"output"`
	Truncated   bool     `json:"truncated"`
	DurationMS  int64    `json:"duration_ms"`
	NetworkUsed bool     `json:"network_used"`
	Hint        string   `json:"hint,omitempty"`
}

// ExtPackage 是隔离前缀中已安装的一个扩展。
type ExtPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ExtListResult 是 runtime-node.ext.list 的结果。
type ExtListResult struct {
	Prefix   string       `json:"prefix"`
	Packages []ExtPackage `json:"packages"`
	Count    int          `json:"count"`
	Hint     string       `json:"hint,omitempty"`
}

// ExtRemoveResult 是 runtime-node.ext.remove 的结果。
type ExtRemoveResult struct {
	Prefix    string   `json:"prefix"`
	Removed   []string `json:"removed"`
	Output    string   `json:"output"`
	Truncated bool     `json:"truncated"`
	Hint      string   `json:"hint,omitempty"`
}

type extInstallParams struct {
	Packages []string `json:"packages"`
}

type extListParams struct{}

type extRemoveParams struct {
	Packages []string `json:"packages"`
}

// ExtManager 是扩展管理执行面；真实实现走 os/exec + 状态目录文件系统，测试可注入假执行面。
type ExtManager interface {
	InstallPackages(prefix string, packages []string) (ExtInstallResult, error)
	ListPackages(prefix string) (ExtListResult, error)
	RemovePackages(prefix string, packages []string) (ExtRemoveResult, error)
}

func extManager(exec Executor) ExtManager {
	if manager, ok := exec.(ExtManager); ok {
		return manager
	}
	return realExtManager{}
}

func callExtInstall(params json.RawMessage, stateRoot string, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p extInstallParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("runtime-node.ext.install 参数非法，请传 JSON 对象: " + err.Error())
	}
	prefix, perr := resolveExtPrefix(stateRoot)
	if perr != nil {
		return nil, perr
	}
	packages, perr := validateExtPackages(p.Packages)
	if perr != nil {
		return nil, perr
	}
	result, err := extManager(exec).InstallPackages(prefix, packages)
	if err != nil {
		return nil, classify(err)
	}
	result.Prefix = prefix
	result.Packages = packages
	result.NetworkUsed = true
	if !result.Truncated && len(result.Output) > MaxNpmOutputBytes {
		result.Output, result.Truncated = truncateExtOutput(result.Output)
	}
	if result.Hint == "" {
		result.Hint = "扩展只装进插件隔离目录（<state>/global/node_modules），不会改动系统全局 npm；如失败请检查网络或镜像源。"
	}
	return marshal(result)
}

func callExtList(params json.RawMessage, stateRoot string, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p extListParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("runtime-node.ext.list 参数非法，请传空 JSON 对象 {}: " + err.Error())
	}
	prefix, perr := resolveExtPrefix(stateRoot)
	if perr != nil {
		return nil, perr
	}
	result, err := extManager(exec).ListPackages(prefix)
	if err != nil {
		return nil, classify(err)
	}
	result.Prefix = prefix
	if result.Packages == nil {
		result.Packages = []ExtPackage{}
	}
	result.Count = len(result.Packages)
	if result.Count == 0 && result.Hint == "" {
		result.Hint = "还没有隔离扩展；请在版本详情里点「管理扩展」安装，包会写进插件状态目录。"
	}
	return marshal(result)
}

func callExtRemove(params json.RawMessage, stateRoot string, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p extRemoveParams
	if err := strictUnmarshal(params, &p); err != nil {
		return nil, invalid("runtime-node.ext.remove 参数非法，请传 JSON 对象: " + err.Error())
	}
	prefix, perr := resolveExtPrefix(stateRoot)
	if perr != nil {
		return nil, perr
	}
	packages, perr := validateExtPackages(p.Packages)
	if perr != nil {
		return nil, perr
	}
	result, err := extManager(exec).RemovePackages(prefix, packages)
	if err != nil {
		return nil, classify(err)
	}
	result.Prefix = prefix
	result.Removed = packages
	if !result.Truncated && len(result.Output) > MaxNpmOutputBytes {
		result.Output, result.Truncated = truncateExtOutput(result.Output)
	}
	if result.Hint == "" {
		result.Hint = "只移除隔离目录内的扩展；系统全局 npm 不受影响。"
	}
	return marshal(result)
}

// resolveExtPrefix 计算隔离前缀并做路径/软链校验：
// 前缀固定为 <state>/global，不接受调用方传路径；词法层拒绝 .. / NUL / 反斜杠，
// 真实层解析最近存在祖先的符号链接后仍须落在状态目录内（挡软链逃逸）。
func resolveExtPrefix(stateRoot string) (string, *pluginapi.Error) {
	trimmed := strings.TrimSpace(stateRoot)
	if trimmed == "" {
		return "", invalid("请配置插件状态目录后再管理扩展")
	}
	if trimmed != stateRoot {
		return "", invalid("请去掉插件状态目录首尾空白后重试")
	}
	if strings.ContainsRune(trimmed, 0) || strings.Contains(trimmed, "\\") {
		return "", invalid("插件状态目录含非法字符（NUL / 反斜杠），请改用正斜杠路径")
	}
	if !filepath.IsAbs(trimmed) {
		return "", invalid("插件状态目录请填绝对路径（以 / 开头）")
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == ".." {
			return "", invalid("插件状态目录含 .. 段，请改用允许目录内的路径")
		}
	}
	root := filepath.Clean(trimmed)
	prefix := filepath.Join(root, DefaultGlobalSubdir)
	if !within(root, prefix) {
		return "", invalid("隔离扩展路径越界，请检查插件状态目录配置")
	}
	realRoot, perr := realAncestor(root)
	if perr != nil {
		return "", perr
	}
	realPrefix, perr := realAncestor(prefix)
	if perr != nil {
		return "", perr
	}
	if !within(realRoot, realPrefix) {
		return "", invalid("隔离扩展目录指向了状态目录之外的位置，请删除该软链后重试")
	}
	return prefix, nil
}

// realAncestor 取最近存在祖先并解析符号链接（不存在时逐层回退到父目录）。
func realAncestor(path string) (string, *pluginapi.Error) {
	ancestor := path
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	real, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", invalid("隔离扩展目录解析失败，请检查路径权限: " + err.Error())
	}
	return real, nil
}

// validateExtPackages 校验包名白名单；空列表直接拒绝。
func validateExtPackages(packages []string) ([]string, *pluginapi.Error) {
	if len(packages) == 0 {
		return nil, invalid("请填写要安装的扩展包名（例如 chalk 或 chalk==5.0.0）")
	}
	if len(packages) > MaxExtPackages {
		return nil, invalid(fmt.Sprintf("扩展包数量 %d 超过上限 %d，请分批操作", len(packages), MaxExtPackages))
	}
	out := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if !extPackageSpecPattern.MatchString(pkg) {
			return nil, invalid(fmt.Sprintf("请检查包名 %q：只接受字母数字 . _ - 和可选的 ==版本，不要写空格、分号、@scope 或 -g 选项", pkg))
		}
		out = append(out, pkg)
	}
	return out, nil
}

// truncateExtOutput 保留首尾各一半并插入截断标记。
func truncateExtOutput(out string) (string, bool) {
	if len(out) <= MaxNpmOutputBytes {
		return out, false
	}
	half := MaxNpmOutputBytes / 2
	return out[:half] + fmt.Sprintf("\n...[输出截断，共 %d 字节]...\n", len(out)) + out[len(out)-half:], true
}

// extCommandRunner 是 npm 命令执行面的最小契约（测试可注入以核对固定 argv）。
type extCommandRunner func(timeout time.Duration, name string, args ...string) (string, error)

// realExtManager 是真实执行面：npm install/uninstall --prefix <state>/global + 直接读 node_modules。
type realExtManager struct {
	runner extCommandRunner
}

func (m realExtManager) commandRunner() extCommandRunner {
	if m.runner != nil {
		return m.runner
	}
	return runExtCommand
}

func (m realExtManager) InstallPackages(prefix string, packages []string) (ExtInstallResult, error) {
	args := append([]string{"install", "--prefix", prefix, "--no-audit", "--no-fund", "--ignore-scripts"}, packages...)
	started := time.Now()
	out, err := m.commandRunner()(ExtCommandTimeout, "npm", args...)
	result := ExtInstallResult{
		Prefix: prefix, Output: out, DurationMS: time.Since(started).Milliseconds(), NetworkUsed: true,
	}
	truncated, _ := truncateExtOutput(out)
	result.Output, result.Truncated = truncated, len(out) > MaxNpmOutputBytes
	if err != nil {
		return result, err
	}
	return result, nil
}

func (m realExtManager) RemovePackages(prefix string, packages []string) (ExtRemoveResult, error) {
	args := append([]string{"uninstall", "--prefix", prefix, "--no-audit", "--no-fund", "--ignore-scripts"}, packages...)
	out, err := m.commandRunner()(ExtCommandTimeout, "npm", args...)
	truncated, wasTruncated := truncateExtOutput(out)
	result := ExtRemoveResult{Prefix: prefix, Output: truncated, Truncated: wasTruncated}
	if err != nil {
		return result, err
	}
	return result, nil
}

func (realExtManager) ListPackages(prefix string) (ExtListResult, error) {
	modules := filepath.Join(prefix, "node_modules")
	entries, err := os.ReadDir(modules)
	if errors.Is(err, os.ErrNotExist) {
		return ExtListResult{Packages: []ExtPackage{}}, nil
	}
	if err != nil {
		return ExtListResult{}, err
	}
	packages := make([]ExtPackage, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "@") {
			scoped, readErr := os.ReadDir(filepath.Join(modules, entry.Name()))
			if readErr != nil {
				continue
			}
			for _, sub := range scoped {
				if sub.IsDir() {
					appendExtPackage(&packages, filepath.Join(modules, entry.Name(), sub.Name()))
				}
			}
			continue
		}
		appendExtPackage(&packages, filepath.Join(modules, entry.Name()))
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	return ExtListResult{Packages: packages}, nil
}

// appendExtPackage 从一个包目录读取 package.json 的 name/version；无 package.json 用目录名兜底。
func appendExtPackage(out *[]ExtPackage, dir string) {
	payload, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		*out = append(*out, ExtPackage{Name: filepath.Base(dir)})
		return
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(payload, &manifest); err != nil {
		*out = append(*out, ExtPackage{Name: filepath.Base(dir)})
		return
	}
	name := manifest.Name
	if name == "" {
		name = filepath.Base(dir)
	}
	*out = append(*out, ExtPackage{Name: name, Version: manifest.Version})
}

// runExtCommand 固定 argv 执行命令（无 shell），合并输出，超时返回 ErrExtTimeout。
func runExtCommand(timeout time.Duration, name string, args ...string) (string, error) {
	if timeout <= 0 {
		timeout = ExtCommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buffer strings.Builder
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return buffer.String(), ErrExtTimeout
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return buffer.String(), fmt.Errorf("找不到可执行文件 %s: %w", name, err)
		}
		return buffer.String(), fmt.Errorf("命令执行失败（%s）: %w", name, err)
	}
	return buffer.String(), nil
}
