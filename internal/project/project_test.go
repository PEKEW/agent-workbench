package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, path, body string) {
	t.Helper()
	f := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(f), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestCreateAndOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "中文 项目")
	name := `测试 "项目"`
	p, err := Create(root, name)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var cfg map[string]any
	b, err := os.ReadFile(filepath.Join(root, "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["project_name"] != name {
		t.Fatalf("name=%v", cfg)
	}
	info, err := p.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != name || len(info.Documents) < 8 || len(info.Warnings) != 0 {
		t.Fatalf("%+v", info)
	}
	q, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	text, err := q.ReadDocument("PLAN.md")
	if err != nil || !strings.Contains(text, "当前") {
		t.Fatalf("%s %v", text, err)
	}
}
func TestCreateRefusesExistingAndInvalidInputs(t *testing.T) {
	root := t.TempDir()
	put(t, root, "keep.txt", "unchanged")
	if p, err := Create(root, "项目"); err == nil {
		p.Close()
		t.Fatal("覆盖了已有目录")
	}
	b, _ := os.ReadFile(filepath.Join(root, "keep.txt"))
	if string(b) != "unchanged" {
		t.Fatal("改变了原文件")
	}
	for _, name := range []string{"", "\n", "bad\nname", "bad\n", strings.Repeat("字", 101)} {
		target := filepath.Join(root, "invalid")
		if p, err := Create(target, name); err == nil {
			p.Close()
			t.Fatalf("接受了 %q", name)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("非法输入创建了目录")
		}
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "missing"), link); err != nil {
		t.Fatal(err)
	}
	if p, err := Create(link, "test"); err == nil {
		p.Close()
		t.Fatal("覆盖符号链接")
	}
}
func TestOpenReadOnlyAndDistinctPaths(t *testing.T) {
	root := t.TempDir()
	put(t, root, "project.json", `{"project_name":"已有项目"}`)
	put(t, root, "PLAN.md", "uncommitted plan")
	put(t, root, "docs/specs/README.md", "spec")
	put(t, root, "docs/reviews/README.md", "review")
	p, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	info, err := p.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Warnings) == 0 {
		t.Fatal("缺项未提示")
	}
	for path, want := range map[string]string{"PLAN.md": "uncommitted plan", "docs/specs/README.md": "spec", "docs/reviews/README.md": "review"} {
		got, err := p.ReadDocument(path)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", path, got, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if string(b) != "uncommitted plan" {
		t.Fatal("打开修改了文件")
	}
	put(t, root, "PLAN.md", "updated")
	b2, err := p.ReadDocument("PLAN.md")
	if err != nil || b2 != "updated" {
		t.Fatal("未读取最新正文")
	}
}
func TestMissingOrMalformedConfig(t *testing.T) {
	for _, config := range []string{"", "{bad", "null", "[]"} {
		t.Run(config, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "PLAN.md", "# 状态")
			if config != "" {
				put(t, root, "project.json", config)
			}
			p, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			i, err := p.Snapshot()
			if err != nil || len(i.Warnings) == 0 {
				t.Fatalf("%+v %v", i, err)
			}
		})
	}
	if p, err := Open(t.TempDir()); err == nil {
		p.Close()
		t.Fatal("接受了无关目录")
	}
}
func TestReadRejectsEscapesSymlinksAndLargeFiles(t *testing.T) {
	root := t.TempDir()
	put(t, root, "PLAN.md", "ok")
	outside := t.TempDir()
	put(t, outside, "secret.md", "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "leak.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}
	p, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, path := range []string{"../secret.md", filepath.Join(outside, "secret.md"), "leak.md", "docs/secret.md", "project.json", ".git/config"} {
		if _, err := p.ReadDocument(path); err == nil {
			t.Fatalf("泄露路径 %s", path)
		}
	}
	put(t, root, "large.md", strings.Repeat("x", 2*1024*1024+1))
	if _, err := p.ReadDocument("large.md"); err == nil {
		t.Fatal("接受超大文件")
	}
	i, err := p.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range i.Documents {
		if d.Path == "leak.md" || d.Path == "docs/secret.md" {
			t.Fatalf("列出了链接 %s", d.Path)
		}
	}
}

func TestDiagramReadBoundary(t *testing.T) {
	root := t.TempDir()
	put(t, root, "PLAN.md", "ok")
	put(t, root, "docs/diagrams/design.html", "<!doctype html><title>计划图</title>")
	put(t, root, "docs/reviews/raw.html", "private")
	p, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	body, err := p.ReadDiagram("docs/diagrams/design.html")
	if err != nil || !strings.Contains(body, "计划图") {
		t.Fatalf("%s %v", body, err)
	}
	for _, path := range []string{"docs/reviews/raw.html", "../outside.html", "PLAN.md"} {
		if _, err := p.ReadDiagram(path); err == nil {
			t.Fatalf("不应读取 %s", path)
		}
	}
}
