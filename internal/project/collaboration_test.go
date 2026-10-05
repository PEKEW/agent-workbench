package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCollaborationPersistenceAndConflicts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project")
	p, err := Create(dir, "讨论")
	if err != nil {
		t.Fatal(err)
	}
	first, err := p.Collaboration()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, ".agent-state")); !os.IsNotExist(err) {
		t.Fatal("read created state")
	}
	thread := Thread{ID: "thread-1", DocumentID: "PLAN.md", Kind: "comment", Status: "open", Messages: []Message{{ID: "message-1", Author: "我", Role: "human", Intent: "comment", Body: "这里的计划需要讨论", CreatedAt: "2026-10-05T10:00:00Z"}}}
	first.Threads = append(first.Threads, thread)
	saved, err := p.SaveCollaboration(first.Generation, first.Threads)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Generation != first.Generation+1 {
		t.Fatal("generation unchanged")
	}
	if _, err = p.SaveCollaboration(first.Generation, first.Threads); !errors.Is(err, ErrCollaborationConflict) {
		t.Fatalf("stale write: %v", err)
	}
	p.Close()
	p, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	restored, err := p.Collaboration()
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != first.ID || len(restored.Threads) != 1 || restored.Threads[0].Messages[0].Body != thread.Messages[0].Body {
		t.Fatal("discussion lost on reopen")
	}
	body, err := p.DocumentState("PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.SaveDocument("PLAN.md", body.Markdown+"\n修订\n", body.Revision); err != nil {
		t.Fatal(err)
	}
	updated, err := p.Collaboration()
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	for _, d := range restored.Documents {
		if d.ID == "PLAN.md" {
			before = d.Revision
		}
	}
	for _, d := range updated.Documents {
		if d.ID == "PLAN.md" {
			after = d.Revision
		}
	}
	if after != before+1 || len(updated.Threads) != 1 {
		t.Fatal("document revision or discussion changed incorrectly")
	}
	updated.Threads[0].Status = "resolved"
	if _, err = p.SaveCollaboration(updated.Generation, updated.Threads); err != nil {
		t.Fatal(err)
	}
}

func TestCollaborationRejectsInvalidStateAndSymlinks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project")
	p, err := Create(dir, "测试")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	thread := Thread{ID: "t", DocumentID: "../../outside.md", Kind: "comment", Status: "open", Messages: []Message{{ID: "m", Author: "我", Role: "human", Intent: "comment", Body: "内容", CreatedAt: "2026-10-05T10:00:00Z"}}}
	if _, err = p.SaveCollaboration(1, []Thread{thread}); err == nil {
		t.Fatal("invalid document accepted")
	}
	thread.DocumentID = "PLAN.md"
	thread.Messages[0].Intent = "execute-shell"
	if _, err = p.SaveCollaboration(1, []Thread{thread}); err == nil {
		t.Fatal("invalid intent accepted")
	}
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(dir, ".agent-state")); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Collaboration(); err == nil {
		t.Fatal("symlink state accepted")
	}
	if _, err = os.Stat(filepath.Join(outside, "collaboration.json")); !os.IsNotExist(err) {
		t.Fatal("outside written")
	}
}

func TestCollaborationSurvivesProjectMove(t *testing.T) {
	parent := t.TempDir()
	oldPath := filepath.Join(parent, "old")
	newPath := filepath.Join(parent, "renamed")
	p, err := Create(oldPath, "项目")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.SaveCollaboration(1, []Thread{})
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	if err = os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	p, err = Open(newPath)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	restored, err := p.Collaboration()
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != saved.ID {
		t.Fatal("move changed workspace identity")
	}
	if _, err = p.SaveCollaboration(restored.Generation, restored.Threads); err != nil {
		t.Fatal(err)
	}
}
