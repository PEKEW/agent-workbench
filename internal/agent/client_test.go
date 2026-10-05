package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func helperCommand() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=TestRuntimeHelper")
	cmd.Env = append(os.Environ(), "AWB_RUNTIME_HELPER=1")
	return cmd
}
func TestRuntimeHelper(t *testing.T) {
	if os.Getenv("AWB_RUNTIME_HELPER") != "1" {
		return
	}
	var mu sync.Mutex
	send := func(value any) { mu.Lock(); defer mu.Unlock(); _ = json.NewEncoder(os.Stdout).Encode(value) }
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 3*1024*1024)
	threadID := "native-thread"
	turnID := ""
	count := 0
	complete := func(id, status string) {
		send(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": threadID, "turnId": id, "item": map[string]any{"id": "reply-" + id, "type": "agentMessage", "text": "原生回复 " + id}}})
		send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": id, "status": status}}})
	}
	for scanner.Scan() {
		var message map[string]any
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			os.Exit(2)
		}
		id := message["id"]
		params, _ := message["params"].(map[string]any)
		method, _ := message["method"].(string)
		switch method {
		case "initialize":
			send(map[string]any{"id": id, "result": map[string]any{"userAgent": "test-runtime"}})
		case "initialized":
		case "thread/start", "thread/resume":
			if method == "thread/resume" && os.Getenv("AWB_RESUME_OTHER_ERROR") == "1" {
				send(map[string]any{"id": id, "error": map[string]any{"code": -32600, "message": "native configuration unavailable"}})
				continue
			}
			if method == "thread/resume" && os.Getenv("AWB_RESUME_MISSING") == "1" {
				send(map[string]any{"id": id, "error": map[string]any{"code": -32600, "message": "no rollout found for thread id " + params["threadId"].(string)}})
				continue
			}
			send(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": threadID}, "model": "test-native", "sandbox": map[string]any{"type": "workspaceWrite"}, "approvalPolicy": "on-request", "approvalsReviewer": "user", "cwd": params["cwd"]}})
		case "turn/start":
			count++
			turnID = time.Now().Format("150405.000000000")
			thisTurn := turnID
			input, _ := params["input"].([]any)
			text := input[0].(map[string]any)["text"].(string)
			send(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": thisTurn, "status": "inProgress"}}})
			send(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": thisTurn, "status": "inProgress"}}})
			if text == "wait" {
				continue
			}
			if text == "approve" {
				send(map[string]any{"id": "approval-1", "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": threadID, "turnId": thisTurn, "itemId": "command-1", "command": "echo approved", "cwd": params["cwd"]}})
				continue
			}
			status := "completed"
			if text == "fail" {
				status = "failed"
			}
			go func() { time.Sleep(100 * time.Millisecond); complete(thisTurn, status) }()
		case "turn/interrupt":
			send(map[string]any{"id": id, "result": map[string]any{}})
			complete(turnID, "interrupted")
		case "test/error":
			send(map[string]any{"id": id, "error": map[string]any{"code": -1, "message": "native failure"}})
		case "test/silent":
		default:
			if method == "" && message["result"] != nil {
				complete(turnID, "completed")
			} else {
				send(map[string]any{"id": id, "error": map[string]any{"code": -32601, "message": "unknown method"}})
			}
		}
	}
	os.Exit(0)
}

func TestClientRoutesResponsesErrorsAndExit(t *testing.T) {
	c, err := startClient(helperCommand(), func(string, json.RawMessage, json.RawMessage) {})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "test"}}); err != nil {
		t.Fatal(err)
	}
	if err = c.Notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Call(ctx, "thread/start", map[string]any{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err = c.Call(ctx, "test/error", nil); err == nil {
		t.Fatal("native error hidden")
	}
	timeout, cancelTimeout := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelTimeout()
	if _, err = c.Call(timeout, "test/silent", nil); err == nil {
		t.Fatal("timeout ignored")
	}
	c.Close()
	if _, err = c.Call(ctx, "thread/start", nil); err == nil {
		t.Fatal("closed process accepted request")
	}
}

func TestGrandchildHelper(t *testing.T) {
	if os.Getenv("AWB_GRANDCHILD_HELPER") != "1" {
		return
	}
	cmd := exec.Command("/bin/sh", "-c", "sleep 60")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		os.Exit(2)
	}
	_, _ = os.Stderr.WriteString("grandchild-ready\n")
	time.Sleep(time.Minute)
	os.Exit(0)
}
func TestClientCloseTerminatesOwnedDescendant(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("Unix process group test")
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestGrandchildHelper")
	cmd.Env = append(os.Environ(), "AWB_GRANDCHILD_HELPER=1")
	ready := make(chan struct{}, 1)
	c, err := startClient(cmd, func(method string, _ json.RawMessage, payload json.RawMessage) {
		if method == "workbench/runtimeDiagnostic" {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProcess(cmd) })
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("helper did not start")
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("close left inherited stdout open in owned descendant")
	}
}
