package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MemoryHook 只展示原生 claude-mem 插件 hook 的具体定义。
type MemoryHook struct {
	Key         string `json:"key"`
	CurrentHash string `json:"currentHash"`
	EventName   string `json:"eventName"`
	Command     string `json:"command"`
	TrustStatus string `json:"trustStatus"`
	PluginID    string `json:"pluginId"`
	Enabled     bool   `json:"enabled"`
	SourcePath  string `json:"sourcePath"`
}
type MemoryHooksState struct {
	Hooks    []MemoryHook `json:"hooks"`
	Version  string       `json:"version"`
	Warnings []string     `json:"warnings"`
}
type MemoryHookIdentity struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
}
type MemoryHooksReview struct {
	Version string               `json:"version"`
	Hooks   []MemoryHookIdentity `json:"hooks"`
}

func memoryPlugin(id string) bool {
	return id == "claude-mem@claude-mem-local" || id == "claude-mem@thedotmack"
}
func (m *Manager) memoryClient(id string) (*client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.sessions[id]
	if m.closed || r == nil || r.client == nil || r.deleting || r.State == "closed" || r.State == "disconnected" {
		return nil, errors.New("请先连接或恢复 Codex 会话")
	}
	return r.client, nil
}
func (m *Manager) memoryHooks(ctx context.Context, c *client) (MemoryHooksState, error) {
	state := MemoryHooksState{Hooks: []MemoryHook{}, Warnings: []string{}}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	b, err := c.Call(ctx, "hooks/list", map[string]any{"cwds": []string{m.cwd}})
	if err != nil {
		return state, err
	}
	var list struct {
		Data []struct {
			Hooks    []MemoryHook `json:"hooks"`
			Warnings []string     `json:"warnings"`
		} `json:"data"`
	}
	if err = json.Unmarshal(b, &list); err != nil {
		return state, err
	}
	for _, entry := range list.Data {
		state.Warnings = append(state.Warnings, entry.Warnings...)
		for _, h := range entry.Hooks {
			if memoryPlugin(h.PluginID) {
				state.Hooks = append(state.Hooks, h)
			}
		}
	}
	b, err = c.Call(ctx, "config/read", map[string]any{"includeLayers": true})
	if err != nil {
		return state, err
	}
	var config struct {
		Layers []struct {
			Name struct {
				Type string `json:"type"`
			} `json:"name"`
			Version string `json:"version"`
		} `json:"layers"`
	}
	if err = json.Unmarshal(b, &config); err != nil {
		return state, err
	}
	for _, l := range config.Layers {
		if l.Name.Type == "user" {
			state.Version = l.Version
		}
	}
	return state, nil
}

// MemoryHooks 从原生运行时查询实际定义和信任状态，不猜测是否采集成功。
func (m *Manager) MemoryHooks(ctx context.Context, id string) (MemoryHooksState, error) {
	c, err := m.memoryClient(id)
	if err != nil {
		return MemoryHooksState{}, err
	}
	return m.memoryHooks(ctx, c)
}

// TrustMemoryHooks 仅信任用户已查看且仍相同的 claude-mem 定义。
func (m *Manager) TrustMemoryHooks(ctx context.Context, id string, review MemoryHooksReview) error {
	if len(review.Hooks) == 0 || len(review.Hooks) > 16 || review.Version == "" {
		return errors.New("请先读取并检查记忆 hooks")
	}
	c, err := m.memoryClient(id)
	if err != nil {
		return err
	}
	current, err := m.memoryHooks(ctx, c)
	if err != nil {
		return err
	}
	if current.Version != review.Version {
		return errors.New("原生配置已变化，请重新查看")
	}
	edits := []map[string]any{}
	seen := map[string]bool{}
	for _, asked := range review.Hooks {
		if seen[asked.Key] {
			return errors.New("重复 hook")
		}
		seen[asked.Key] = true
		found := false
		for _, h := range current.Hooks {
			if h.Key == asked.Key && h.CurrentHash == asked.Hash && h.Enabled && h.TrustStatus != "managed" {
				found = true
				break
			}
		}
		if !found {
			return errors.New("hook 已变化、已停用或不属于记忆插件")
		}
		edits = append(edits, map[string]any{"keyPath": fmt.Sprintf("hooks.state.%q.trusted_hash", asked.Key), "value": asked.Hash, "mergeStrategy": "replace"})
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_, err = c.Call(ctx, "config/batchWrite", map[string]any{"edits": edits, "expectedVersion": review.Version, "reloadUserConfig": true})
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.sessions[id]; r != nil {
		payload, _ := json.Marshal(review)
		return m.recordLocked(r, "workbench/memoryHooksTrusted", nil, payload)
	}
	return nil
}
