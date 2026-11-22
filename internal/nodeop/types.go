// Package nodeop 实现 runtime-node 插件的方法面：检测宿主已安装的 Node.js、
// 汇总单个版本详情、扫描 Node 项目与健康检查。
//
// 安全口径：只读——不执行项目脚本、不写宿主文件；所有外部命令固定 argv + 5s 超时；
// root 参数必须落在白名单目录内并做符号链接二次校验。
package nodeop

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// 采集与安全常量。
const (
	// CommandTimeout 是单条外部命令（node/npm）的硬超时。
	CommandTimeout = 5 * time.Second
	// MaxPackageJSONBytes 是 package.json 的读取上限（只读前 64 KiB，防巨大文件）。
	MaxPackageJSONBytes = 64 * 1024
	// MaxScanDepth 是项目扫描的目录深度上限（root 为 0，最多再下探 3 层）。
	MaxScanDepth = 3
	// MaxScanEntries 是项目扫描的目录条目上限。
	MaxScanEntries = 200
	// NoNodeHint 是未检测到 Node.js 时的降级提示（versions 不报错）。
	NoNodeHint = "未检测到 Node.js，可从 https://nodejs.org 安装或用 nvm"
)

// Source 标识一个 Node 可执行文件的发现渠道。
type Source string

const (
	// SourcePath 表示从 PATH 中找到（node / nodejs）。
	SourcePath Source = "path"
	// SourceKnownPath 表示从常见安装路径找到。
	SourceKnownPath Source = "known_path"
	// SourceNVM 表示从 ~/.nvm/versions/node/*/bin/node 找到。
	SourceNVM Source = "nvm"
)

// 健康检查状态取值。
const (
	StatusPass = "pass"
	StatusWarn = "warn"
	StatusFail = "fail"
)

// FaultKind 标识一次故障注入的类别（basic 级骨架的故障注入契约）。
type FaultKind string

const (
	FaultNone    FaultKind = ""
	FaultKill    FaultKind = "kill"
	FaultTimeout FaultKind = "timeout"
	FaultDisk    FaultKind = "disk"
	FaultCgroup  FaultKind = "cgroup"
)

// FaultError 是执行面注入的故障错误，Call 将其映射为协议错误码。
type FaultError struct {
	Kind    FaultKind
	Message string
}

// Error 实现 error 接口，输出 "<类别>: <说明>"。
func (e *FaultError) Error() string { return fmt.Sprintf("%s: %s", e.Kind, e.Message) }

// NewFaultError 构造给定类别的故障错误。
func NewFaultError(kind FaultKind, message string) *FaultError {
	return &FaultError{Kind: kind, Message: message}
}

// 执行面哨兵错误：用于把"没装 Node"和"指定版本没装"映射为不同协议错误码。
var (
	// ErrNodeNotInstalled 表示宿主一个 Node 都没有。
	ErrNodeNotInstalled = errors.New("未检测到 Node.js")
	// ErrVersionNotInstalled 表示请求的版本不在已安装列表内。
	ErrVersionNotInstalled = errors.New("指定 Node 版本未安装")
)

// CommandRunner 是外部命令执行面的最小契约：固定 argv、无 shell、带超时。
// pathDir 非空时把该目录前置到子进程 PATH（保证 npm 的 shebang 用配套 node）。
type CommandRunner interface {
	Run(name string, pathDir string, args ...string) (string, error)
}

// Executor 是方法执行面：生产实现真实探测宿主；测试可注入故障执行器。
type Executor interface {
	Versions() (VersionsResult, error)
	Info(version string) (InfoResult, error)
	Projects(root string) (ProjectsResult, error)
	Doctor(root string) (DoctorResult, error)
}

// NodeVersion 是一个已安装的 Node 可执行文件。
type NodeVersion struct {
	Version   string `json:"version"`
	Path      string `json:"path"`
	Source    Source `json:"source"`
	IsDefault bool   `json:"is_default"`
}

// VersionsResult 是 runtime-node.versions 的结构化结果。
// 未检测到任何 Node 时 versions 为空数组、count=0、hint 给出安装指引（不报错）。
type VersionsResult struct {
	Versions       []NodeVersion `json:"versions"`
	DefaultVersion string        `json:"default_version"`
	Count          int           `json:"count"`
	Hint           string        `json:"hint,omitempty"`
}

// InfoResult 是 runtime-node.info 的结构化结果。
// npm/V8/OpenSSL 探测失败时对应字段为空串、global_packages 为 0（不放大为整次失败）。
type InfoResult struct {
	Version        string `json:"version"`
	NodePath       string `json:"node_path"`
	NpmPath        string `json:"npm_path"`
	NpmVersion     string `json:"npm_version"`
	V8             string `json:"v8"`
	OpenSSL        string `json:"openssl"`
	GlobalPackages int    `json:"global_packages"`
}

// Project 是一个含 package.json 的 Node 项目条目。
type Project struct {
	Path              string `json:"path"`
	Name              string `json:"name"`
	Version           string `json:"version"`
	EnginesNode       string `json:"engines_node"`
	DependenciesCount int    `json:"dependencies_count"`
	HasLock           bool   `json:"has_lock"`
}

// ProjectsResult 是 runtime-node.projects 的结构化结果。
// 单个 package.json 超过上限/解析失败时跳过并写入 warnings，不中断整次扫描。
type ProjectsResult struct {
	Projects []Project `json:"projects"`
	Count    int       `json:"count"`
	Warnings []string  `json:"warnings"`
}

// Check 是一条健康检查。
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// DoctorResult 是 runtime-node.doctor 的结构化结果：ok = 没有任何 fail 项。
type DoctorResult struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

// versionsParams / infoParams / projectsParams / doctorParams 是四个方法的显式入参形状。
type versionsParams struct{}

type infoParams struct {
	Version string `json:"version"`
}

type projectsParams struct {
	Root string `json:"root"`
}

type doctorParams struct {
	Root string `json:"root"`
}

// packageManifest 是 package.json 中本插件关心的字段（dependencies 保留原始对象再数键）。
type packageManifest struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Engines      enginesField    `json:"engines"`
	Dependencies json.RawMessage `json:"dependencies"`
}

// enginesField 是 package.json.engines 的显式子集。
type enginesField struct {
	Node string `json:"node"`
}

// npmGlobalList 是 `npm ls -g --depth=0 --json` 中用于计数的字段。
type npmGlobalList struct {
	Dependencies json.RawMessage `json:"dependencies"`
}
