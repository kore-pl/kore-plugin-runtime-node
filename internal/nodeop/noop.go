package nodeop

// noopExecutor 是骨架口径的默认执行面：只回显/返回白名单，不真起进程、不真占端口。
type noopExecutor struct{}

func (noopExecutor) Versions() ([]string, error) { return noopVersions(), nil }

func (noopExecutor) AllocatePort(preferred int) (int, error) { return noopAllocatePort(preferred), nil }

// executorFunc 是 Executor 的函数式适配器，供测试注入自定义行为（故障/越界）。
type executorFunc struct {
	versions     func() ([]string, error)
	allocatePort func(int) (int, error)
}

func (e executorFunc) Versions() ([]string, error) {
	if e.versions != nil {
		return e.versions()
	}
	return noopVersions(), nil
}

func (e executorFunc) AllocatePort(preferred int) (int, error) {
	if e.allocatePort != nil {
		return e.allocatePort(preferred)
	}
	return noopAllocatePort(preferred), nil
}
