package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"time"
)

// NativeThread 是本机 Codex 已保存的原生会话摘要，用于新建对话框的接入选择。
type NativeThread struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt"`
}

// ListNativeThreads 临时启动一个原生进程列出本机已保存的 thread；新→旧排序，最多 20 条。
// 仅用于挑选接入目标，不创建也不依赖工作台会话存储。
func ListNativeThreads(ctx context.Context, cwd string, factory func() *exec.Cmd) ([]NativeThread, error) {
	if factory == nil {
		return nil, errors.New("原生运行时不可用")
	}
	command := factory()
	command.Dir = cwd
	c, err := startClient(command, func(string, json.RawMessage, json.RawMessage) {})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	requestContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if _, err = c.Call(requestContext, "initialize", map[string]any{"clientInfo": map[string]string{"name": "agent_workbench", "title": "Agent Workbench", "version": "0.2.0"}}); err != nil {
		return nil, err
	}
	if err = c.Notify("initialized", map[string]any{}); err != nil {
		return nil, err
	}
	value, err := c.Call(requestContext, "thread/list", map[string]any{"limit": 20})
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			UpdatedAt int64  `json:"updatedAt"`
		} `json:"data"`
	}
	if err = json.Unmarshal(value, &result); err != nil {
		return nil, err
	}
	threads := []NativeThread{}
	for i := len(result.Data) - 1; i >= 0; i-- {
		row := result.Data[i]
		if row.ID == "" {
			continue
		}
		threads = append(threads, NativeThread{ID: row.ID, Name: row.Name, UpdatedAt: time.Unix(row.UpdatedAt, 0).UTC().Format(time.RFC3339Nano)})
	}
	return threads, nil
}
