package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionStorageDoesNotWriteOnReadAndRejectsSymlinks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project")
	p, err := Create(dir, "测试")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err = p.OpenSessionStorage(false); !os.IsNotExist(err) {
		t.Fatalf("unexpected state read: %v", err)
	}
	if _, err = os.Stat(filepath.Join(dir, ".agent-state")); !os.IsNotExist(err) {
		t.Fatal("read created metadata")
	}
	external := t.TempDir()
	if err = os.Symlink(external, filepath.Join(dir, ".agent-state")); err != nil {
		t.Fatal(err)
	}
	if _, err = p.OpenSessionStorage(true); err == nil {
		t.Fatal("symlink state accepted")
	}
	if files, err := os.ReadDir(external); err != nil || len(files) != 0 {
		t.Fatal("external state modified")
	}
}
