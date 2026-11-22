// Package nodeop 实现 runtime-node 插件的方法面：参数校验与结构化结果。
// 本包只做逻辑校验与执行面调用，不真起 node 进程、不真占用端口（骨架口径）。
package nodeop

import "fmt"

// 版本白名单（提案 §2.1 批2）：只接受 Node 18 / 20 / 22。
const (
	minPort   = 1024
	maxPort   = 65535
	version18 = "18"
	version20 = "20"
	version22 = "22"
)

// allowedVersions 是 runtime-node.versions 返回的受支持版本白名单。
var allowedVersions = []string{version18, version20, version22}

// FaultKind 标识一次故障注入的类别（basic 级只覆盖 kill / timeout）。
type FaultKind string

const (
	FaultNone    FaultKind = ""
	FaultKill    FaultKind = "kill"
	FaultTimeout FaultKind = "timeout"
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

// Executor 是 node 操作的执行面；骨架实现为 noop，测试可注入故障执行器。
// 骨架口径下两个方法都不真执行，Executor 仅提供故障注入的契约位置。
type Executor interface {
	Versions() ([]string, error)
	AllocatePort(preferred int) (int, error)
}

// versionsParams / allocatePortParams 是两个方法的显式入参形状。
type versionsParams struct {
	Family string `json:"family"`
}

type allocatePortParams struct {
	Preferred int `json:"preferred"`
}

// VersionsResult 是 runtime-node.versions 的结构化结果。
type VersionsResult struct {
	Versions []string `json:"versions"`
	Count    int      `json:"count"`
	Applied  bool     `json:"applied"`
}

// AllocatePortResult 是 runtime-node.allocatePort 的结构化结果。
// 骨架口径下 allocated=false，port 回显的是校验通过的请求端口，不代表真占用。
type AllocatePortResult struct {
	Port      int  `json:"port"`
	Allocated bool `json:"allocated"`
	Applied   bool `json:"applied"`
}
