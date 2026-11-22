package nodeop

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// Call 执行一个 node 方法：先校验方法名与参数，再调用执行面并结构化结果。
// exec 为 nil 时使用 noopExecutor（骨架口径，不真起进程、不真占端口）。
func Call(method string, params json.RawMessage, exec Executor) (json.RawMessage, *pluginapi.Error) {
	verb, ok := businessVerb(method)
	if !ok {
		return nil, errNotFound(method)
	}
	if exec == nil {
		exec = noopExecutor{}
	}
	switch verb {
	case "versions":
		return callVersions(params, exec)
	case "allocatePort":
		return callAllocatePort(params, exec)
	}
	return nil, errNotFound(method)
}

// businessVerb 把 node.<verb> 或裸 <verb> 归一为动词名。
func businessVerb(method string) (string, bool) {
	verb := method
	if strings.HasPrefix(method, "node.") {
		verb = strings.TrimPrefix(method, "node.")
	}
	switch verb {
	case "versions", "allocatePort":
		return verb, true
	}
	return "", false
}

// strictUnmarshal 只接受 JSON 对象入参（拒绝数组/标量/null/空载荷）。
func strictUnmarshal(params json.RawMessage, out any) error {
	trimmed := bytes.TrimSpace(params)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("参数必须是 JSON 对象")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("参数含多余内容")
	}
	return nil
}

// marshal 把结构化结果编码为 result 载荷。
func marshal(v any) (json.RawMessage, *pluginapi.Error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, internalErr("序列化结果失败: " + err.Error())
	}
	return data, nil
}

// classify 把执行面错误映射为协议错误码：两类基础故障均为 E_UPSTREAM。
func classify(err error) *pluginapi.Error {
	var fe *FaultError
	if errors.As(err, &fe) {
		switch fe.Kind {
		case FaultKill:
			return upstream("进程被信号 9 终止: " + fe.Message)
		case FaultTimeout:
			return upstream("调用超时: " + fe.Message)
		}
	}
	return internalErr("执行面失败: " + err.Error())
}

func invalid(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_INVALID, Message: msg}
}

func errNotFound(method string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_NOTFOUND, Message: "未知方法: " + method}
}

func upstream(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_UPSTREAM, Message: msg}
}

func internalErr(msg string) *pluginapi.Error {
	return &pluginapi.Error{Code: pluginapi.E_INTERNAL, Message: msg}
}
