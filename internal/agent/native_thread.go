package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// NativeThread 是本机 Codex 已保存的原生会话摘要，用于新建对话框的接入选择。
type NativeThread struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Preview   string `json:"preview,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

// nativeThreadLimit 是接入对话框一次列出的会话数。
const nativeThreadLimit = 50

// ListNativeThreads 临时启动一个原生进程列出本项目目录下已保存的 thread，按最近更新时间新→旧排序。
// 原生默认按创建时间排序，早先创建、近期继续使用的会话会排在后面，因此显式按 updated_at 排序；
// 旧版本不支持 cwd 或 sortKey 时退回不带筛选的请求。仅用于挑选接入目标，不创建也不依赖工作台会话存储。
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
	value, err := c.Call(requestContext, "thread/list", map[string]any{"limit": nativeThreadLimit, "cwd": cwd, "sortKey": "updated_at"})
	if err != nil {
		var protocol *nativeRPCError
		if !errors.As(err, &protocol) {
			return nil, err
		}
		if value, err = c.Call(requestContext, "thread/list", map[string]any{"limit": nativeThreadLimit}); err != nil {
			return nil, err
		}
	}
	var result struct {
		Data []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Preview   string `json:"preview"`
			UpdatedAt int64  `json:"updatedAt"`
		} `json:"data"`
	}
	if err = json.Unmarshal(value, &result); err != nil {
		return nil, err
	}
	sort.SliceStable(result.Data, func(i, j int) bool { return result.Data[i].UpdatedAt > result.Data[j].UpdatedAt })
	threads := []NativeThread{}
	for _, row := range result.Data {
		if row.ID == "" {
			continue
		}
		preview := []rune(strings.Join(strings.Fields(row.Preview), " "))
		if len(preview) > 80 {
			preview = append(preview[:80], '…')
		}
		threads = append(threads, NativeThread{ID: row.ID, Name: row.Name, Preview: string(preview), UpdatedAt: time.Unix(row.UpdatedAt, 0).UTC().Format(time.RFC3339Nano)})
	}
	return threads, nil
}
