package agent

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestMemoryHooksRejectsForeignOrChangedApproval(t *testing.T) {
	m, _ := testManager(t)
	s, err := m.Create(context.Background(), StartOptions{Name: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := m.MemoryHooks(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks.Hooks) != 1 {
		t.Fatal(hooks)
	}
	if err = m.TrustMemoryHooks(context.Background(), s.ID, MemoryHooksReview{Version: hooks.Version, Hooks: []MemoryHookIdentity{{Key: "other", Hash: "H"}}}); err == nil {
		t.Fatal("trusted foreign hook")
	}
	review := MemoryHooksReview{Version: hooks.Version, Hooks: []MemoryHookIdentity{{Key: hooks.Hooks[0].Key, Hash: "changed"}}}
	if err = m.TrustMemoryHooks(context.Background(), s.ID, review); err == nil {
		t.Fatal("trusted changed hook")
	}
	review.Hooks[0].Hash = hooks.Hooks[0].CurrentHash
	if err = m.TrustMemoryHooks(context.Background(), s.ID, review); err != nil {
		t.Fatal(err)
	}
}

// 原生探测仅在显式启用时运行；信任只写临时 CODEX_HOME。
func TestInstalledCodexMemoryHooksTrust(t *testing.T) {
	if os.Getenv("AWB_REAL_CODEX_MEMORY_HOOKS") != "1" {
		t.Skip("explicit native hooks verification")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	native := t.TempDir()
	cache := filepath.Join(native, "plugins/cache/claude-mem-local/claude-mem/13.34.2")
	source := filepath.Join(home, ".codex/plugins/cache/claude-mem-local/claude-mem/13.34.2")
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		target := filepath.Join(cache, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	config := `model = "gpt-6.1-sol"
[features]
hooks = true
[marketplaces.claude-mem-local]
source_type = "local"
source = ` + strconv.Quote(filepath.Join(home, ".claude/plugins/marketplaces/thedotmack")) + `
[plugins."claude-mem@claude-mem-local"]
enabled = true
`
	if err = os.WriteFile(filepath.Join(native, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	settings := `{"CLAUDE_MEM_PROVIDER":"codex","CLAUDE_MEM_FOLDER_CLAUDEMD_ENABLED":"false","CLAUDE_MEM_CODEX_TRANSCRIPT_INGESTION":"false"}`
	_ = os.WriteFile(filepath.Join(data, "settings.json"), []byte(settings), 0600)
	dir := t.TempDir()
	root, _ := os.OpenRoot(dir)
	m, err := NewManager(dir, root, func() *exec.Cmd {
		c := exec.Command("/opt/homebrew/bin/codex", "app-server", "--listen", "stdio://")
		c.Env = append(os.Environ(), "CODEX_HOME="+native, "CLAUDE_MEM_DATA_DIR="+data, "CLAUDE_MEM_WORKER_PORT=37703")
		return c
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s, err := m.Create(context.Background(), StartOptions{Name: "native-memory", Sandbox: "read-only", ApprovalPolicy: "on-request"})
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := m.MemoryHooks(context.Background(), s.ID)
	if err != nil || len(hooks.Hooks) != 5 {
		t.Fatal("native hooks", hooks, err)
	}
	review := MemoryHooksReview{Version: hooks.Version}
	for _, h := range hooks.Hooks {
		review.Hooks = append(review.Hooks, MemoryHookIdentity{Key: h.Key, Hash: h.CurrentHash})
	}
	if err = m.TrustMemoryHooks(context.Background(), s.ID, review); err != nil {
		t.Fatal("native trust", err)
	}
	trusted, err := m.MemoryHooks(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range trusted.Hooks {
		if h.TrustStatus != "trusted" {
			t.Fatal("native trust not effective", h.EventName, h.TrustStatus)
		}
	}
}
