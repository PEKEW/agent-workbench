package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// CommandAuthorization 是工作台回应原生审批的参数前缀规则。
type CommandAuthorization struct {
	ID        string   `json:"id"`
	Prefix    []string `json:"prefix"`
	SessionID string   `json:"sessionId,omitempty"`
}
type CommandAuthorizations struct {
	Generation int                    `json:"generation"`
	Rules      []CommandAuthorization `json:"rules"`
}

func (m *Manager) Authorizations() CommandAuthorizations {
	m.mu.Lock()
	defer m.mu.Unlock()
	return copyAuthorizations(m.policy)
}
func copyAuthorizations(p CommandAuthorizations) CommandAuthorizations {
	b, _ := json.Marshal(p)
	var q CommandAuthorizations
	_ = json.Unmarshal(b, &q)
	if q.Rules == nil {
		q.Rules = []CommandAuthorization{}
	}
	return q
}
func (m *Manager) loadAuthorizations() error {
	m.policy.Rules = []CommandAuthorization{}
	info, err := m.safeStat("command-authorizations.json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return errors.New("授权文件无效")
	}
	b, err := m.root.ReadFile("command-authorizations.json")
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &m.policy); err != nil {
		return err
	}
	return m.validateRules(m.policy.Rules)
}
func shell(name string) bool {
	switch filepath.Base(name) {
	case "sh", "bash", "zsh", "dash", "fish", "ksh", "csh", "tcsh":
		return true
	}
	return false
}
func (m *Manager) validateRules(rules []CommandAuthorization) error {
	if len(rules) > 64 {
		return errors.New("最多 64 条授权")
	}
	ids := map[string]bool{}
	for _, r := range rules {
		if r.ID == "" || len(r.ID) > 200 || ids[r.ID] || len(r.Prefix) == 0 || len(r.Prefix) > 32 {
			return errors.New("授权 ID 或前缀无效")
		}
		ids[r.ID] = true
		if r.SessionID != "" && m.sessions[r.SessionID] == nil {
			return errors.New("授权会话不存在")
		}
		if shell(r.Prefix[0]) || strings.Contains(r.Prefix[0], "=") {
			return errors.New("不能授权整个 shell 解释器或环境赋值")
		}
		for _, a := range r.Prefix {
			if a == "" || len(a) > 512 || strings.ContainsAny(a, "\x00\n\r$`;&|<>\\*?[]()") {
				return errors.New("请填写明确的简单命令参数")
			}
		}
	}
	return nil
}
func (m *Manager) storePolicyLocked(p CommandAuthorizations) error {
	if _, err := m.safeStat("command-authorizations.json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	name := ".authorization-" + rand.Text()
	f, err := m.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer m.root.Remove(name)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = m.root.Rename(name, "command-authorizations.json")
	}
	if err == nil {
		m.policy = copyAuthorizations(p)
	}
	return err
}

// SaveAuthorizations 保存项目内授权，旧 generation 不得覆盖新配置。
func (m *Manager) SaveAuthorizations(generation int, rules []CommandAuthorization) (CommandAuthorizations, error) {
	m.policyMu.Lock()
	defer m.policyMu.Unlock()
	m.mu.Lock()
	if m.closed || generation != m.policy.Generation {
		m.mu.Unlock()
		return CommandAuthorizations{}, errors.New("授权已变化，请重新载入")
	}
	if err := m.validateRules(rules); err != nil {
		m.mu.Unlock()
		return CommandAuthorizations{}, err
	}
	p := copyAuthorizations(CommandAuthorizations{Generation: generation + 1, Rules: rules})
	if err := m.storePolicyLocked(p); err != nil {
		m.mu.Unlock()
		return CommandAuthorizations{}, err
	}
	var pending [][2]string
	for id, r := range m.sessions {
		for _, q := range r.Requests {
			pending = append(pending, [2]string{id, q.ID})
		}
	}
	m.mu.Unlock()
	for _, q := range pending {
		go m.autoApproveCommand(q[0], q[1])
	}
	return p, nil
}

// simpleCommand 拒绝展开、管道和复合 shell，仅解析字面参数。
func simpleCommand(command string) ([]string, error) {
	parse := func(s string) ([]string, error) {
		for _, c := range s {
			if c > 127 && (unicode.IsSpace(c) || unicode.IsControl(c)) {
				return nil, errors.New("不支持的空白或控制字符")
			}
		}
		if strings.ContainsAny(s, "\x00\n\r$`;&|<>\\*?[]()") {
			return nil, errors.New("复杂命令需人工审批")
		}
		var args []string
		var word strings.Builder
		var quote rune
		started := false
		for _, c := range s {
			if quote != 0 {
				if c == quote {
					quote = 0
				} else {
					word.WriteRune(c)
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				started = true
			} else if c == ' ' || c == '\t' {
				if started {
					args = append(args, word.String())
					word.Reset()
					started = false
				}
			} else {
				word.WriteRune(c)
				started = true
			}
		}
		if quote != 0 {
			return nil, errors.New("引号未闭合")
		}
		if started {
			args = append(args, word.String())
		}
		if len(args) == 0 || args[0] == "" || strings.Contains(args[0], "=") {
			return nil, errors.New("命令无效")
		}
		return args, nil
	}
	args, err := parse(command)
	if err != nil {
		return nil, err
	}
	if shell(args[0]) {
		if len(args) != 3 || (args[1] != "-c" && args[1] != "-lc") {
			return nil, errors.New("不支持的 shell 包装")
		}
		args, err = parse(args[2])
		if err != nil {
			return nil, err
		}
		if shell(args[0]) {
			return nil, errors.New("嵌套 shell 需人工审批")
		}
	}
	return args, nil
}
func (m *Manager) authorizedRuleLocked(r *runtimeSession, q Request) (CommandAuthorization, bool) {
	if q.Method != "item/commandExecution/requestApproval" {
		return CommandAuthorization{}, false
	}
	var p struct {
		ThreadID      string            `json:"threadId"`
		TurnID        string            `json:"turnId"`
		Command       string            `json:"command"`
		Cwd           string            `json:"cwd"`
		Kind          string            `json:"kind"`
		EnvironmentID *string           `json:"environmentId"`
		Network       json.RawMessage   `json:"networkApprovalContext"`
		Permissions   json.RawMessage   `json:"additionalPermissions"`
		Decisions     []json.RawMessage `json:"availableDecisions"`
	}
	if json.Unmarshal(q.Params, &p) != nil || p.ThreadID != r.ThreadID || p.TurnID == "" || p.TurnID != r.ActiveTurnID || (p.Kind != "" && p.Kind != "command") || p.EnvironmentID != nil && *p.EnvironmentID != "" || nonNull(p.Network) || nonNull(p.Permissions) {
		return CommandAuthorization{}, false
	}
	if p.Decisions != nil {
		accept := false
		for _, d := range p.Decisions {
			if string(d) == `"accept"` {
				accept = true
			}
		}
		if !accept {
			return CommandAuthorization{}, false
		}
	}
	if !filepath.IsAbs(p.Cwd) {
		return CommandAuthorization{}, false
	}
	cwd, err := filepath.EvalSymlinks(p.Cwd)
	if err != nil {
		return CommandAuthorization{}, false
	}
	root, err := filepath.EvalSymlinks(m.cwd)
	if err != nil {
		return CommandAuthorization{}, false
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return CommandAuthorization{}, false
	}
	args, err := simpleCommand(p.Command)
	if err != nil {
		return CommandAuthorization{}, false
	}
	for _, rule := range m.policy.Rules {
		if rule.SessionID != "" && rule.SessionID != r.ID || len(args) < len(rule.Prefix) {
			continue
		}
		match := true
		for i, a := range rule.Prefix {
			if args[i] != a {
				match = false
				break
			}
		}
		if match {
			return rule, true
		}
	}
	return CommandAuthorization{}, false
}
func nonNull(b json.RawMessage) bool { return len(b) > 0 && string(b) != "null" }
func (m *Manager) autoApproveCommand(id, requestID string) {
	m.mu.Lock()
	r := m.sessions[id]
	if r == nil || m.closed {
		m.mu.Unlock()
		return
	}
	var req Request
	for _, q := range r.Requests {
		if q.ID == requestID {
			req = q
			break
		}
	}
	rule, ok := m.authorizedRuleLocked(r, req)
	gen := m.policy.Generation
	m.mu.Unlock()
	if !ok {
		return
	}
	guard := func() error {
		if m.policy.Generation != gen {
			return errors.New("授权已撤销或修改")
		}
		current, ok := m.authorizedRuleLocked(r, req)
		if !ok || current.ID != rule.ID {
			return errors.New("授权不再匹配")
		}
		return nil
	}
	_ = m.answer(context.Background(), id, requestID, json.RawMessage(`{"decision":"accept"}`), guard, &rule)
}
