package nodeop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 创建目录（如不存在）并写入内容。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestProjectsScan 真实扫描：正常项目、坏 JSON、无 package.json、嵌套项目与跳过目录。
func TestProjectsScan(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "good", "package.json"),
		`{"name":"good-app","version":"1.2.3","engines":{"node":">=18"},"dependencies":{"express":"^4.0.0","dotenv":"^16"}}`)
	writeFile(t, filepath.Join(base, "good", "package-lock.json"), `{}`)
	writeFile(t, filepath.Join(base, "bad", "package.json"), `{ this is not json`)
	writeFile(t, filepath.Join(base, "none", "readme.md"), `no package.json here`)
	writeFile(t, filepath.Join(base, "nested", "deep", "app", "package.json"), `{"name":"nested-app"}`)
	writeFile(t, filepath.Join(base, "too-deep", "a", "b", "c", "package.json"), `{"name":"too-deep"}`)
	writeFile(t, filepath.Join(base, "node_modules", "dep", "package.json"), `{"name":"dep"}`)
	writeFile(t, filepath.Join(base, ".cache", "package.json"), `{"name":"hidden"}`)

	result, err := (&Runtime{}).Projects(base)
	if err != nil {
		t.Fatalf("Projects 失败: %v", err)
	}
	if result.Count != 2 || len(result.Projects) != 2 {
		t.Fatalf("期望 2 个项目（good + 深度 3 的 nested），实际=%+v", result.Projects)
	}
	byName := map[string]Project{}
	for _, project := range result.Projects {
		byName[project.Name] = project
	}
	good, ok := byName["good-app"]
	if !ok {
		t.Fatalf("缺少 good-app: %+v", result.Projects)
	}
	if good.EnginesNode != ">=18" || good.DependenciesCount != 2 || !good.HasLock || good.Version != "1.2.3" {
		t.Fatalf("good-app 字段错误: %+v", good)
	}
	if !strings.HasSuffix(good.Path, string(filepath.Separator)+"good") {
		t.Fatalf("路径错误: %q", good.Path)
	}
	if _, ok := byName["nested-app"]; !ok {
		t.Fatalf("深度 3 的嵌套项目未被扫描: %+v", result.Projects)
	}
	if !hasWarning(result.Warnings, "解析失败") {
		t.Fatalf("坏 JSON 未写 warnings: %v", result.Warnings)
	}
	if _, ok := byName["too-deep"]; ok {
		t.Fatalf("深度 4 的项目不应被扫描")
	}
	if _, ok := byName["dep"]; ok {
		t.Fatalf("node_modules 下的项目不应被扫描")
	}
	if _, ok := byName["hidden"]; ok {
		t.Fatalf("隐藏目录下的项目不应被扫描")
	}
}

// TestProjectsSkipsOversizedPackageJSON 超过 64 KiB 的 package.json 跳过并告警（只读前 64 KiB）。
func TestProjectsSkipsOversizedPackageJSON(t *testing.T) {
	base := t.TempDir()
	payload := `{"name":"huge","description":"` + strings.Repeat("x", MaxPackageJSONBytes+16) + `"}`
	writeFile(t, filepath.Join(base, "huge", "package.json"), payload)
	result, err := (&Runtime{}).Projects(base)
	if err != nil {
		t.Fatalf("Projects 失败: %v", err)
	}
	if result.Count != 0 {
		t.Fatalf("超大 package.json 应被跳过: %+v", result.Projects)
	}
	if !hasWarning(result.Warnings, "超过") {
		t.Fatalf("超大文件未写 warnings: %v", result.Warnings)
	}
}

// TestProjectsEntryLimit 目录条目超过上限时截断并告警，且不会无界扫描。
func TestProjectsEntryLimit(t *testing.T) {
	base := t.TempDir()
	for i := 0; i < MaxScanEntries+10; i++ {
		if err := os.Mkdir(filepath.Join(base, "d"+padIndex(i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	result, err := (&Runtime{}).Projects(base)
	if err != nil {
		t.Fatalf("Projects 失败: %v", err)
	}
	if !hasWarning(result.Warnings, "上限") {
		t.Fatalf("条目上限未写 warnings: %v", result.Warnings)
	}
}

// TestProjectsDoesNotFollowSymlinkDir 扫描不跟随目录符号链接（防止逃出 root）。
func TestProjectsDoesNotFollowSymlinkDir(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "package.json"), `{"name":"outside"}`)
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Skipf("当前文件系统不支持符号链接: %v", err)
	}
	result, err := (&Runtime{}).Projects(base)
	if err != nil {
		t.Fatalf("Projects 失败: %v", err)
	}
	if result.Count != 0 {
		t.Fatalf("不应跟随目录符号链接: %+v", result.Projects)
	}
}

// hasWarning 判断 warnings 中是否含指定关键词。
func hasWarning(warnings []string, keyword string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, keyword) {
			return true
		}
	}
	return false
}

// padIndex 把目录序号补齐为 3 位，保证创建顺序稳定。
func padIndex(value int) string {
	digits := []byte{byte('0' + value/100%10), byte('0' + value/10%10), byte('0' + value%10)}
	return string(digits)
}
