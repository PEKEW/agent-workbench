package agent

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

func (m *Manager) safeStat(path string) (fs.FileInfo, error) {
	if !fs.ValidPath(path) || strings.Contains(path, "\\") {
		return nil, errors.New("状态路径无效")
	}
	var info fs.FileInfo
	parts := strings.Split(path, "/")
	for i := range parts {
		var err error
		info, err = m.root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("不访问状态符号链接")
		}
	}
	return info, nil
}
func (m *Manager) load() error {
	entries, err := fs.ReadDir(m.root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := entry.Name() + "/state.json"
		info, err := m.safeStat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 64*1024*1024 {
			return errors.New("会话状态过大或类型无效")
		}
		data, err := m.root.ReadFile(path)
		if err != nil {
			return err
		}
		var state Session
		if err = json.Unmarshal(data, &state); err != nil {
			return err
		}
		if state.ID != entry.Name() || state.Queue == nil {
			return errors.New("会话状态格式无效")
		}
		if err = validateOptions(&state.Options); err != nil {
			return err
		}
		if state.State != "closed" {
			state.State = "disconnected"
		}
		state.Requests = []Request{}
		state.ActiveTurnID = ""
		state.QueuePaused = len(state.Queue) > 0
		r := &runtimeSession{Session: state, answering: map[string]bool{}, completedTurns: map[string]bool{}, recent: []Event{}}
		m.sessions[state.ID] = r
		if state.LastTurnID != "" {
			r.completedTurns[state.LastTurnID] = true
		}
		events, err := m.readEventsObserved(state.ID, 0, 0, func(event Event) {
			if event.Method != "turn/completed" {
				return
			}
			var p struct {
				ThreadID string `json:"threadId"`
				Turn     struct {
					ID string `json:"id"`
				} `json:"turn"`
			}
			if json.Unmarshal(event.Params, &p) == nil && p.Turn.ID != "" && (p.ThreadID == "" || p.ThreadID == r.ThreadID) {
				r.completedTurns[p.Turn.ID] = true
			}
		})
		if err != nil {
			return err
		}
		if len(events) > 0 {
			r.Sequence = events[len(events)-1].Sequence
		}
		if err = m.restoreDiscussionLocked(r); err != nil {
			return err
		}
		if len(events) > 1000 {
			events = events[len(events)-1000:]
		}
		r.recent = events
	}
	return nil
}
func (m *Manager) persistLocked(r *runtimeSession) error {
	if err := m.root.Mkdir(r.ID, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if _, err := m.safeStat(r.ID); err != nil {
		return err
	}
	if _, err := m.safeStat(r.ID + "/state.json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(r.Session, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 64*1024*1024 {
		return errors.New("会话状态超过 64 MiB")
	}
	parent, err := m.root.OpenRoot(r.ID)
	if err != nil {
		return err
	}
	defer parent.Close()
	name := ".state-" + rand.Text() + ".tmp"
	f, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer parent.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return parent.Rename(name, "state.json")
}
func (m *Manager) recordLocked(r *runtimeSession, method string, id, params json.RawMessage) error {
	path := r.ID + "/events.jsonl"
	if _, err := m.safeStat(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := m.root.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	event := Event{Sequence: r.Sequence + 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Method: method, RequestID: id, Params: params}
	if len(event.Params) == 0 {
		event.Params = json.RawMessage(`{}`)
	}
	err = json.NewEncoder(f).Encode(event)
	if err == nil && strings.HasPrefix(method, "workbench/") {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	r.Sequence++
	r.recent = append(r.recent, event)
	if len(r.recent) > 1000 {
		r.recent = r.recent[len(r.recent)-1000:]
	}
	return nil
}
func (m *Manager) readEvents(id string, after, limit int) ([]Event, error) {
	return m.readEventsObserved(id, after, limit, nil)
}
func (m *Manager) readEventsObserved(id string, after, limit int, observe func(Event)) ([]Event, error) {
	info, err := m.safeStat(id + "/events.jsonl")
	if errors.Is(err, fs.ErrNotExist) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("日志不是普通文件")
	}
	f, err := m.root.Open(id + "/events.jsonl")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return scanEventsObserved(f, after, limit, observe)
}
func scanEvents(reader io.Reader, after, limit int) ([]Event, error) {
	return scanEventsObserved(reader, after, limit, nil)
}
func scanEventsObserved(reader io.Reader, after, limit int, observe func(Event)) ([]Event, error) {
	result := []Event{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 10*1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, errors.New("日志含不完整或无效记录，原文件已保留")
		}
		if observe != nil {
			observe(event)
		}
		if event.Sequence > after {
			result = append(result, event)
			if limit == 0 && len(result) > 1000 {
				result = result[len(result)-1000:]
			}
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, scanner.Err()
}

// Events 按序号补读日志，最多返回 1000 条；旧记录保留在磁盘。
func (m *Manager) Events(id string, after int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.sessions[id]
	if r == nil || after < 0 {
		return nil, errors.New("会话或事件序号无效")
	}
	if len(r.recent) > 0 && after >= r.recent[0].Sequence-1 {
		events := []Event{}
		for _, e := range r.recent {
			if e.Sequence > after {
				events = append(events, e)
			}
		}
		return events, nil
	}
	return m.readEvents(id, after, 1000)
}

// canRecreateEmptyLocked 只允许完整日志证明从未收到输入的会话重建原生连接。
// 不依赖最近事件缓存：旧输入即使已超过 1000 条仍禁止替换 thread。
func (m *Manager) canRecreateEmptyLocked(r *runtimeSession) (bool, error) {
	if m.closed || r.State == "closed" || r.ActiveInput != nil || r.ActiveTurnID != "" || r.LastTurnStatus != "" || len(r.Queue) != 0 {
		return false, nil
	}
	after := 0
	for {
		events, err := m.readEvents(r.ID, after, 1000)
		if err != nil {
			return false, err
		}
		for _, event := range events {
			if event.Sequence != after+1 {
				return false, nil
			}
			after = event.Sequence
			if event.Method == "workbench/inputAccepted" || strings.HasPrefix(event.Method, "turn/") || strings.HasPrefix(event.Method, "item/") {
				return false, nil
			}
		}
		if len(events) < 1000 {
			return after > 0 && after == r.Sequence, nil
		}
	}
}
