package project

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const maxCollaborationBytes = 16 * 1024 * 1024
const collaborationPath = ".agent-state/collaboration.json"

var ErrCollaborationConflict = errors.New("讨论已被其他页面修改；请刷新后重试，当前输入仍保留")

type Message struct {
	ID        string         `json:"id"`
	Author    string         `json:"author"`
	Role      string         `json:"role"`
	Intent    string         `json:"intent"`
	Body      string         `json:"body"`
	CreatedAt string         `json:"createdAt"`
	Source    *MessageSource `json:"source,omitempty"`
}

// MessageSource 保存运行时来源，供自动与手动回写去重。
type MessageSource struct {
	SessionID string `json:"sessionId"`
	ThreadID  string `json:"threadId"`
	TurnID    string `json:"turnId"`
	ItemID    string `json:"itemId"`
}
type Anchor struct {
	Quote    string `json:"quote"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Revision int    `json:"revision"`
}
type Thread struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"documentId"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	DueDate    string    `json:"dueDate"`
	Anchor     *Anchor   `json:"anchor"`
	Messages   []Message `json:"messages"`
}
type CollaborationDocument struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	Title           string `json:"title"`
	Kind            string `json:"kind"`
	Revision        int    `json:"revision"`
	ContentRevision string `json:"contentRevision"`
	Markdown        string `json:"markdown"`
}
type CollaborationState struct {
	Format           string                  `json:"format"`
	Version          int                     `json:"version"`
	ID               string                  `json:"id"`
	Title            string                  `json:"title"`
	Generation       int                     `json:"generation"`
	Documents        []CollaborationDocument `json:"documents"`
	Threads          []Thread                `json:"threads"`
	AppliedChangeIDs []string                `json:"appliedChangeIds"`
}
type collaborationFile struct {
	ID         string                           `json:"id"`
	Generation int                              `json:"generation"`
	Threads    []Thread                         `json:"threads"`
	Documents  map[string]CollaborationDocument `json:"documents"`
}

func (p *Project) collaborationFile() (collaborationFile, error) {
	initial := collaborationFile{ID: "workbench-" + revisionOf(p.path), Generation: 1, Threads: []Thread{}, Documents: map[string]CollaborationDocument{}}
	if _, err := p.safeStat(".agent-state"); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return initial, nil
		}
		return initial, err
	}
	info, err := p.safeStat(collaborationPath)
	if errors.Is(err, fs.ErrNotExist) {
		return initial, nil
	}
	if err != nil {
		return initial, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxCollaborationBytes {
		return initial, errors.New("讨论状态不是普通文件或超过 16 MiB")
	}
	f, err := p.root.Open(collaborationPath)
	if err != nil {
		return initial, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCollaborationBytes+1))
	if err != nil {
		return initial, err
	}
	if len(data) > maxCollaborationBytes || !utf8.Valid(data) {
		return initial, errors.New("讨论状态内容无效")
	}
	var saved collaborationFile
	if err = json.Unmarshal(data, &saved); err != nil {
		return initial, fmt.Errorf("讨论状态无法解析，原文件已保留：%w", err)
	}
	if !validID(saved.ID) || !strings.HasPrefix(saved.ID, "workbench-") || saved.Generation < 1 || saved.Threads == nil || saved.Documents == nil {
		return initial, errors.New("讨论状态格式不匹配，原文件已保留")
	}
	if err = validateThreads(saved.Threads, nil); err != nil {
		return initial, err
	}
	return saved, nil
}

// Collaboration 只读载入讨论；正文版本随内容哈希变化，不写入项目。
func (p *Project) Collaboration() (CollaborationState, error) {
	saved, err := p.collaborationFile()
	if err != nil {
		return CollaborationState{}, err
	}
	info, err := p.Snapshot()
	if err != nil {
		return CollaborationState{}, err
	}
	state := CollaborationState{Format: "agent-docs-workspace", Version: 1, ID: saved.ID, Title: info.Name, Generation: saved.Generation, Documents: []CollaborationDocument{}, Threads: saved.Threads, AppliedChangeIDs: []string{}}
	for _, d := range info.Documents {
		previous := saved.Documents[d.Path]
		current := CollaborationDocument{ID: d.Path, Source: d.Path, Title: d.Path, Kind: "document", Revision: previous.Revision, ContentRevision: previous.ContentRevision}
		if current.Revision < 1 {
			current.Revision = 1
		}
		if body, e := p.DocumentState(d.Path); e == nil {
			if previous.ContentRevision != "" && previous.ContentRevision != body.Revision {
				current.Revision++
			}
			current.ContentRevision = body.Revision
		}
		state.Documents = append(state.Documents, current)
	}
	return state, nil
}

func validText(s string, max int) bool { return utf8.ValidString(s) && len(s) <= max }
func validID(s string) bool            { return validText(s, 200) && strings.TrimSpace(s) != "" }
func validateThreads(threads []Thread, known map[string]bool) error {
	if len(threads) > 10000 {
		return errors.New("讨论数量超过上限")
	}
	ids, messages := map[string]bool{}, map[string]bool{}
	for _, t := range threads {
		if !validID(t.ID) || ids[t.ID] || !allowedDocument(t.DocumentID) || known != nil && !known[t.DocumentID] || t.Kind != "comment" && t.Kind != "memo" || t.Status != "open" && t.Status != "resolved" || len(t.Messages) == 0 {
			return errors.New("讨论格式、ID 或文档引用无效")
		}
		ids[t.ID] = true
		if t.DueDate != "" {
			if _, err := time.Parse("2006-01-02", t.DueDate); err != nil {
				return errors.New("备忘日期无效")
			}
		}
		if a := t.Anchor; a != nil {
			if !validText(a.Quote, 20000) || strings.TrimSpace(a.Quote) == "" || !validText(a.Before, 500) || !validText(a.After, 500) || a.Revision < 1 {
				return errors.New("引用格式无效")
			}
		}
		for _, m := range t.Messages {
			if source := m.Source; source != nil && (!validID(source.SessionID) || !validID(source.ThreadID) || !validID(source.TurnID) || !validID(source.ItemID)) {
				return errors.New("消息来源无效")
			}
			if !validID(m.ID) || messages[m.ID] || !validText(m.Author, 320) || strings.TrimSpace(m.Author) == "" || m.Role != "human" && m.Role != "agent" || m.Intent != "comment" && m.Intent != "rebuttal" && m.Intent != "decision" || !validText(m.Body, MaxDocumentBytes) || strings.TrimSpace(m.Body) == "" {
				return errors.New("消息格式或 ID 无效")
			}
			if _, err := time.Parse(time.RFC3339Nano, m.CreatedAt); err != nil {
				return errors.New("消息日期无效")
			}
			messages[m.ID] = true
		}
	}
	return nil
}

// SaveCollaboration 只保存协作状态，正文通过文档接口单独保存。
// 调用方须串行化同一项目的写入； generation 防止旧页面覆盖新讨论。
func (p *Project) SaveCollaboration(generation int, threads []Thread) (CollaborationState, error) {
	state, err := p.Collaboration()
	if err != nil {
		return state, err
	}
	if state.Generation != generation {
		return state, ErrCollaborationConflict
	}
	known := map[string]bool{}
	for _, d := range state.Documents {
		known[d.ID] = true
	}
	// 已删除文档上的旧讨论仍可回复或删除，不允许新建任意路径引用。
	for _, t := range state.Threads {
		known[t.DocumentID] = true
	}
	if err = validateThreads(threads, known); err != nil {
		return state, err
	}
	saved := collaborationFile{ID: state.ID, Generation: state.Generation + 1, Threads: threads, Documents: map[string]CollaborationDocument{}}
	for _, d := range state.Documents {
		saved.Documents[d.ID] = d
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return state, err
	}
	if len(data) > maxCollaborationBytes {
		return state, errors.New("讨论状态超过 16 MiB")
	}
	if err = p.root.Mkdir(".agent-state", 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return state, err
	}
	if _, err = p.safeStat(".agent-state"); err != nil {
		return state, err
	}
	parent, err := p.root.OpenRoot(".agent-state")
	if err != nil {
		return state, err
	}
	defer parent.Close()
	name := ".collaboration-" + rand.Text() + ".tmp"
	f, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return state, err
	}
	defer parent.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return state, err
	}
	if closeErr != nil {
		return state, closeErr
	}
	if err = parent.Rename(name, "collaboration.json"); err != nil {
		return state, err
	}
	state.Generation = saved.Generation
	state.Threads = threads
	return state, nil
}
