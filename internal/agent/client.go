// Package agent 管理 Codex 原生协议，不执行模型工具。
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
)

type rpcResult struct {
	value json.RawMessage
	err   error
}
type client struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	writeMu   sync.Mutex
	mu        sync.Mutex
	next      int
	pending   map[string]chan rpcResult
	done      chan struct{}
	closeOnce sync.Once
}

func startClient(cmd *exec.Cmd, onEvent func(string, json.RawMessage, json.RawMessage)) (*client, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	configureProcess(cmd)
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, err
	}
	c := &client{cmd: cmd, stdin: stdin, pending: map[string]chan rpcResult{}, done: make(chan struct{})}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 8*1024*1024)
		for scanner.Scan() {
			var m struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
				payload, _ := json.Marshal(map[string]string{"error": "原生协议返回无效 JSON"})
				onEvent("workbench/protocolError", nil, payload)
				stopProcess(cmd)
				break
			}
			if m.Method != "" {
				onEvent(m.Method, m.ID, m.Params)
				continue
			}
			c.mu.Lock()
			waiter := c.pending[string(m.ID)]
			delete(c.pending, string(m.ID))
			c.mu.Unlock()
			if waiter != nil {
				result := rpcResult{value: m.Result}
				if m.Error != nil {
					result.err = fmt.Errorf("Codex %d：%s", m.Error.Code, m.Error.Message)
				}
				waiter <- result
			}
		}
		if scanner.Err() != nil {
			payload, _ := json.Marshal(map[string]string{"error": scanner.Err().Error()})
			onEvent("workbench/protocolError", nil, payload)
			stopProcess(cmd)
		}
	}()
	go func() {
		defer readers.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			payload, _ := json.Marshal(map[string]string{"text": scanner.Text()})
			onEvent("workbench/runtimeDiagnostic", nil, payload)
		}
	}()
	go func() {
		readers.Wait()
		err := cmd.Wait()
		message := "原生进程已退出"
		if err != nil {
			message = err.Error()
		}
		payload, _ := json.Marshal(map[string]string{"error": message})
		onEvent("workbench/runtimeExited", nil, payload)
		c.mu.Lock()
		close(c.done)
		for key, waiter := range c.pending {
			waiter <- rpcResult{err: errors.New(message)}
			delete(c.pending, key)
		}
		c.mu.Unlock()
	}()
	return c, nil
}
func (c *client) write(value any) error { return c.writeChecked(value, nil) }
func (c *client) writeChecked(value any, check func() error) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return errors.New("原生进程已关闭")
	default:
	}
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	return json.NewEncoder(c.stdin).Encode(value)
}
func (c *client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	waiter := make(chan rpcResult, 1)
	key := strconv.Itoa(id)
	c.pending[key] = waiter
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	if err := c.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case result := <-waiter:
		return result.value, result.err
	case <-ctx.Done():
		return nil, fmt.Errorf("原生请求超时或已取消；不会自动重放：%w", ctx.Err())
	case <-c.done:
		return nil, errors.New("原生进程已关闭")
	}
}
func (c *client) Notify(method string, params any) error {
	return c.write(map[string]any{"method": method, "params": params})
}
func (c *client) Reply(id json.RawMessage, result any) error {
	return c.write(map[string]any{"id": id, "result": result})
}
func (c *client) Close() {
	c.closeOnce.Do(func() { stopProcess(c.cmd); _ = c.stdin.Close(); <-c.done })
}

func (c *client) replyIf(id json.RawMessage, result any, check func() error) error {
	return c.writeChecked(map[string]any{"id": id, "result": result}, check)
}
