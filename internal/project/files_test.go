package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func openFixture(t *testing.T, files map[string]string) (*Project, string) {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		put(t, root, name, body)
	}
	p, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p, root
}

func TestOverviewListsVisibleCodeFiles(t *testing.T) {
	p, root := openFixture(t, map[string]string{"PLAN.md": "# 计划", "docs/a.md": "a", "cmd/main.go": "package main", "go.mod": "module x", ".env": "SECRET=1", ".git/config": "x", "web/.cache/x.js": "x", "node_modules/pkg/index.js": "x", "internal/x/README.md": "# 内部"})
	if err := os.Symlink(filepath.Join(root, "go.mod"), filepath.Join(root, "linked.mod")); err != nil {
		t.Fatal(err)
	}
	info, err := p.Overview()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range info.Files {
		got[f.Path] = true
	}
	for _, want := range []string{"cmd/main.go", "go.mod", "internal/x/README.md"} {
		if !got[want] {
			t.Fatalf("缺少 %s：%v", want, info.Files)
		}
	}
	for _, hidden := range []string{"PLAN.md", "docs/a.md", ".env", ".git/config", "web/.cache/x.js", "node_modules/pkg/index.js", "linked.mod"} {
		if got[hidden] {
			t.Fatalf("不应列出 %s", hidden)
		}
	}
	snapshot, _ := p.Snapshot()
	if snapshot.Files != nil {
		t.Fatal("轮询用的 Snapshot 不应遍历代码文件")
	}
}

func TestReadTextFileAndExternalPathBoundaries(t *testing.T) {
	p, root := openFixture(t, map[string]string{"PLAN.md": "x", "cmd/main.go": "package main", ".env": "SECRET=1", "bin/tool": "\xff\xfe"})
	content, err := p.ReadTextFile("cmd/main.go")
	if err != nil || content.Text != "package main" {
		t.Fatal(content, err)
	}
	for _, name := range []string{".env", "../outside", "/etc/passwd", "cmd/../.env", "bin/tool"} {
		if _, err = p.ReadTextFile(name); err == nil {
			t.Fatalf("读取 %s 未被拒绝", name)
		}
	}
	resolved, _ := filepath.EvalSymlinks(root)
	target, err := p.ExternalPath("cmd/main.go")
	if err != nil || target != filepath.Join(resolved, "cmd", "main.go") {
		t.Fatal(target, err)
	}
	for _, name := range []string{".env", "cmd", "missing.go", "../x"} {
		if _, err = p.ExternalPath(name); err == nil {
			t.Fatalf("外部打开 %s 未被拒绝", name)
		}
	}
}

func TestCreateRenameDeleteDocument(t *testing.T) {
	p, root := openFixture(t, map[string]string{"PLAN.md": "# 计划", "docs/keep.md": "keep", "docs/reviews/discussion/frozen.md": "frozen"})
	created, err := p.CreateDocument("docs/notes/新文档.md")
	if err != nil || created.Markdown != "# 新文档\n" || !created.Editable {
		t.Fatal(created, err)
	}
	for name, want := range map[string]error{"docs/keep.md": ErrExists, "docs/reviews/discussion/new.md": ErrReadOnly} {
		if _, err = p.CreateDocument(name); !errors.Is(err, want) {
			t.Fatalf("%s：%v", name, err)
		}
	}
	for _, name := range []string{"src/a.md", "docs/a.txt", "docs/.hidden.md", "../a.md"} {
		if _, err = p.CreateDocument(name); err == nil {
			t.Fatalf("新建 %s 未被拒绝", name)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "docs/keep.md")); string(b) != "keep" {
		t.Fatal("已有文件被覆盖")
	}

	state, _ := p.Collaboration()
	thread := Thread{ID: "t1", DocumentID: "docs/notes/新文档.md", Kind: "comment", Status: "open", Messages: []Message{{ID: "m1", Author: "我", Role: "human", Intent: "comment", Body: "意见", CreatedAt: "2026-01-01T00:00:00Z"}}}
	if _, err = p.SaveCollaboration(state.Generation, []Thread{thread}); err != nil {
		t.Fatal(err)
	}
	if err = p.RenameDocument("docs/notes/新文档.md", "docs/keep.md", created.Revision); !errors.Is(err, ErrExists) {
		t.Fatalf("重命名覆盖了已有文件：%v", err)
	}
	if err = p.RenameDocument("docs/notes/新文档.md", "docs/renamed.md", "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧版本重命名未拒绝：%v", err)
	}
	if err = p.RenameDocument("docs/notes/新文档.md", "docs/renamed.md", created.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "docs/notes/新文档.md")); !os.IsNotExist(err) {
		t.Fatal("原文件仍存在")
	}
	state, _ = p.Collaboration()
	if len(state.Threads) != 1 || state.Threads[0].DocumentID != "docs/renamed.md" {
		t.Fatalf("讨论未随文档迁移：%+v", state.Threads)
	}

	renamed, _ := p.DocumentState("docs/renamed.md")
	if err = p.DeleteDocument("docs/renamed.md", "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧版本删除未拒绝：%v", err)
	}
	frozen, _ := p.DocumentState("docs/reviews/discussion/frozen.md")
	if err = p.DeleteDocument("docs/reviews/discussion/frozen.md", frozen.Revision); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("只读区域被删除：%v", err)
	}
	if err = p.DeleteDocument("docs/renamed.md", renamed.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "docs/renamed.md")); !os.IsNotExist(err) {
		t.Fatal("文件未删除")
	}
	state, _ = p.Collaboration()
	if len(state.Threads) != 1 {
		t.Fatal("删除文档不应删除讨论")
	}
}
