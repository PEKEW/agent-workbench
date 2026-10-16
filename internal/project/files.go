package project

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const maxFiles = 5000
const maxFileVisits = 50000

// 依赖与构建产物目录体积大且不是项目源码，不进入文件导航。
var skippedDirectories = map[string]bool{"node_modules": true, "__pycache__": true, "venv": true, "dist": true, "target": true}

// visibleFile 拒绝隐藏路径，避免网页列出或读取 .env、.git 和工作台状态。
func visibleFile(name string) bool {
	if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

// Overview 返回文档列表及项目内其他可见文件；轮询接口只使用 Snapshot，避免反复遍历整个代码库。
func (p *Project) Overview() (Info, error) {
	info, err := p.Snapshot()
	if err != nil {
		return info, err
	}
	known := map[string]bool{}
	for _, d := range append(append([]Document{}, info.Documents...), info.Diagrams...) {
		known[d.Path] = true
	}
	info.Files = []Document{}
	visits := 0
	err = fs.WalkDir(p.root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			info.Warnings = append(info.Warnings, "无法读取目录项："+name)
			return nil
		}
		if name == "." {
			return nil
		}
		visits++
		if visits > maxFileVisits || len(info.Files) >= maxFiles {
			info.Warnings = append(info.Warnings, "代码文件数量超过扫描上限，列表已截断")
			return fs.SkipAll
		}
		if strings.HasPrefix(d.Name(), ".") || d.IsDir() && skippedDirectories[d.Name()] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && !known[name] {
			info.Files = append(info.Files, Document{Path: name})
		}
		return nil
	})
	if err != nil {
		return Info{}, err
	}
	sort.Slice(info.Files, func(i, j int) bool { return info.Files[i].Path < info.Files[j].Path })
	return info, nil
}

// FileContent 是代码或其他文本文件的只读内容。
type FileContent struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// ReadTextFile 只读返回项目内可见的 UTF-8 文本文件。
func (p *Project) ReadTextFile(name string) (FileContent, error) {
	if !visibleFile(name) {
		return FileContent{}, errors.New("仅允许读取项目内非隐藏的相对路径")
	}
	data, err := p.read(name)
	if err != nil {
		return FileContent{}, err
	}
	return FileContent{Path: name, Text: string(data)}, nil
}

// ExternalPath 返回交给外部编辑器的绝对路径；目标须为项目内非隐藏的普通文件。
func (p *Project) ExternalPath(name string) (string, error) {
	if !visibleFile(name) {
		return "", errors.New("仅允许打开项目内非隐藏的相对路径")
	}
	info, err := p.safeStat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("目标不是普通文件")
	}
	return filepath.Join(p.path, filepath.FromSlash(name)), nil
}

var ErrExists = errors.New("目标文件已存在，不会覆盖")

// checkWritableDocument 核对路径规则、只读区域与已读取版本，供重命名和删除共用。
func (p *Project) checkWritableDocument(name, revision string) error {
	if !allowedDocument(name) || !visibleFile(name) {
		return errors.New("仅允许操作根目录或 docs 下的 Markdown")
	}
	if !p.editable(name) {
		return ErrReadOnly
	}
	current, err := p.DocumentState(name)
	if err != nil {
		return err
	}
	if revision == "" || current.Revision != revision {
		return ErrConflict
	}
	return nil
}

// checkNewDocument 确认目标路径可写且尚不存在，并按需创建 docs 下的父目录。
func (p *Project) checkNewDocument(name string) error {
	if !allowedDocument(name) || !visibleFile(name) {
		return errors.New("新文档须为根目录或 docs 下的 .md 路径")
	}
	if !p.editable(name) {
		return ErrReadOnly
	}
	if _, err := p.safeStat(name); err == nil {
		return ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if dir := path.Dir(name); dir != "." {
		if err := p.root.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if info, err := p.safeStat(dir); err != nil || !info.IsDir() {
			return errors.New("父目录无效或包含符号链接")
		}
	}
	return nil
}

// CreateDocument 新建 Markdown，拒绝覆盖已有文件，不自动提交 Git。
func (p *Project) CreateDocument(name string) (DocumentContent, error) {
	if err := p.checkNewDocument(name); err != nil {
		return DocumentContent{}, err
	}
	body := "# " + strings.TrimSuffix(path.Base(name), path.Ext(name)) + "\n"
	f, err := p.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, fs.ErrExist) {
		return DocumentContent{}, ErrExists
	}
	if err != nil {
		return DocumentContent{}, err
	}
	_, err = f.WriteString(body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = p.root.Remove(name)
		return DocumentContent{}, err
	}
	return p.DocumentState(name)
}

// RenameDocument 以硬链接加删除完成不覆盖的重命名，并把讨论随文档迁移。
func (p *Project) RenameDocument(name, target, revision string) error {
	if name == target {
		return errors.New("新路径与原路径相同")
	}
	if err := p.checkWritableDocument(name, revision); err != nil {
		return err
	}
	if err := p.checkNewDocument(target); err != nil {
		return err
	}
	if err := p.root.Link(name, target); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrExists
		}
		return err
	}
	if err := p.root.Remove(name); err != nil {
		_ = p.root.Remove(target)
		return err
	}
	return p.moveThreads(name, target)
}

// DeleteDocument 删除已读取版本的 Markdown；文档上的讨论保留，可继续查看或删除。
func (p *Project) DeleteDocument(name, revision string) error {
	if err := p.checkWritableDocument(name, revision); err != nil {
		return err
	}
	return p.root.Remove(name)
}
