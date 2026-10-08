package agent

import (
	"crypto/rand"
	"errors"
	"io/fs"
)

// DeleteSession 删除工作台记录，保留文档、讨论及原生历史。
func (m *Manager) DeleteSession(id string) error { return m.DeleteSessionWithOptions(id, false) }

// DeleteSessionWithOptions 可显式放弃断开后无法确认送达的旧输入。
func (m *Manager) DeleteSessionWithOptions(id string, abandonUnknown bool) error {
	m.mu.Lock()
	r := m.sessions[id]
	if m.closed || r == nil || r.deleting {
		m.mu.Unlock()
		return errors.New("会话不存在或正在清理")
	}
	if r.busy || r.State == "starting" || r.State == "running" || r.State == "waiting" || (r.ActiveInput != nil && !(abandonUnknown && r.client == nil && (r.State == "closed" || r.State == "disconnected"))) || r.ActiveTurnID != "" || len(r.Queue) > 0 {
		m.mu.Unlock()
		return errors.New("请先结束回合并清空或完成队列")
	}
	for _, reply := range r.Replies {
		if !reply.Synced && !reply.Discarded {
			m.mu.Unlock()
			return errors.New("讨论结果尚未回写，请先处理")
		}
	}
	err := fs.WalkDir(m.root.FS(), id, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		_, err = m.safeStat(path)
		return err
	})
	if err != nil {
		m.mu.Unlock()
		return err
	}
	r.deleting = true
	r.State = "closed"
	if err = m.persistLocked(r); err != nil {
		r.deleting = false
		m.mu.Unlock()
		return err
	}
	c := r.client
	m.mu.Unlock()
	if c != nil {
		c.Close()
	}
	m.policyMu.Lock()
	defer m.policyMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	defer func() { r.deleting = false }()
	p := copyAuthorizations(m.policy)
	p.Rules = []CommandAuthorization{}
	for _, rule := range m.policy.Rules {
		if rule.SessionID != id {
			p.Rules = append(p.Rules, rule)
		}
	}
	if len(p.Rules) != len(m.policy.Rules) {
		p.Generation++
		if err = m.storePolicyLocked(p); err != nil {
			return err
		}
	}
	tombstone := ".deleted-" + rand.Text()
	if err = m.root.Rename(id, tombstone); err != nil {
		return err
	}
	delete(m.sessions, id)
	return m.root.RemoveAll(tombstone)
}
