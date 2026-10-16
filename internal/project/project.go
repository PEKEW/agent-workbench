// Package project 管理 semi-harness 项目目录，不执行项目脚本。
package project

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxDocumentBytes = 2 * 1024 * 1024
const maxDocuments = 2000

//go:embed all:template
var templateFiles embed.FS

// Document 使用项目内路径区分同名文档。
type Document struct {
	Path string `json:"path"`
}

// Info 是可供网页展示的项目当前状态。
type Info struct {
	Diagrams  []Document `json:"diagrams"`
	Name      string     `json:"name"`
	Root      string     `json:"root"`
	Documents []Document `json:"documents"`
	Files     []Document `json:"files,omitempty"`
	Warnings  []string   `json:"warnings"`
}

// Project 持有受根目录约束的文件访问句柄。
type Project struct {
	root *os.Root
	path string
}

// Create 在不存在的目录中创建项目，失败时清理本次创建的内容。
func Create(path, name string) (*Project, error) {
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return nil, errors.New("项目名称不能包含控制字符")
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return nil, errors.New("项目名须为 1–100 个字符，且不能包含控制字符")
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("请填写新项目目录")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.Mkdir(absolute, 0755); err != nil {
		return nil, fmt.Errorf("无法创建目录，请确认父目录存在且目标不存在：%w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(absolute)
		}
	}()
	err = fs.WalkDir(templateFiles, "template", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "template" {
			return nil
		}
		relative := strings.TrimPrefix(path, "template/")
		target := filepath.Join(absolute, filepath.FromSlash(relative))
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := templateFiles.ReadFile(path)
		if err != nil {
			return err
		}
		if relative == "project.json" {
			var cfg map[string]any
			if err = json.Unmarshal(data, &cfg); err != nil {
				return err
			}
			cfg["project_name"] = name
			data, err = json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			data = append(data, '\n')
		} else {
			data = []byte(strings.ReplaceAll(string(data), "{{PROJECT}}", name))
		}
		return os.WriteFile(target, data, 0644)
	})
	if err != nil {
		return nil, fmt.Errorf("创建项目文件失败：%w", err)
	}
	p, err := Open(absolute)
	if err != nil {
		return nil, err
	}
	success = true
	return p, nil
}

// Open 只读识别已有目录；缺少部分模板文件时仍允许接续。
func Open(path string) (*Project, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("请填写已有项目目录")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("无法打开项目目录：%w", err)
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("无法打开项目目录：%w", err)
	}
	p := &Project{root: root, path: absolute}
	_, cfgErr := p.config()
	recognized := cfgErr == nil
	for _, marker := range []string{"AGENTS.md", "PLAN.md", "docs/specs"} {
		if info, e := p.safeStat(marker); e == nil && (info.Mode().IsRegular() || marker == "docs/specs" && info.IsDir()) {
			recognized = true
		}
	}
	if !recognized {
		root.Close()
		return nil, errors.New("未识别为 semi-harness 项目，请选择含 project.json、AGENTS.md、PLAN.md 或 docs/specs 的目录")
	}
	return p, nil
}

// Close 释放根目录句柄。
func (p *Project) Close() error { return p.root.Close() }

func (p *Project) safeStat(path string) (fs.FileInfo, error) {
	if !fs.ValidPath(path) || strings.Contains(path, "\\") {
		return nil, errors.New("无效的项目相对路径")
	}
	parts := strings.Split(path, "/")
	var info fs.FileInfo
	for i := range parts {
		var err error
		info, err = p.root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("不读取符号链接")
		}
	}
	return info, nil
}
func (p *Project) read(path string) ([]byte, error) {
	info, err := p.safeStat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("目标不是普通文件")
	}
	if info.Size() > MaxDocumentBytes {
		return nil, errors.New("文档超过 2 MiB 读取上限")
	}
	f, err := p.root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("目标不是普通文件")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxDocumentBytes {
		return nil, errors.New("文档超过 2 MiB 读取上限")
	}
	if !utf8.Valid(data) {
		return nil, errors.New("文档不是有效 UTF-8 文本")
	}
	return data, nil
}
func (p *Project) config() (map[string]any, error) {
	b, err := p.read("project.json")
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err = json.Unmarshal(b, &config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, errors.New("配置须为 JSON 对象")
	}
	return config, nil
}
func allowedDocument(path string) bool {
	return fs.ValidPath(path) && !strings.Contains(path, "\\") && strings.EqualFold(filepath.Ext(path), ".md") && (!strings.Contains(path, "/") || strings.HasPrefix(path, "docs/"))
}

// ReadDocument 读取根目录或 docs 内的 Markdown 与交互图源文件，拒绝目录越界和符号链接。
func (p *Project) ReadDocument(path string) (string, error) {
	if !allowedDocument(path) && !p.allowedDiagramSource(path) {
		return "", errors.New("仅允许读取根目录或 docs 下的 Markdown，或 docs/diagrams 下的交互图源文件")
	}
	b, err := p.read(path)
	return string(b), err
}

// Snapshot 重新读取配置与目录，返回文档列表和缺项提示。
func (p *Project) Snapshot() (Info, error) {
	info := Info{Diagrams: []Document{}, Name: filepath.Base(p.path), Root: p.path, Documents: []Document{}, Warnings: []string{}}
	if cfg, err := p.config(); err != nil {
		info.Warnings = append(info.Warnings, "project.json 缺失、无法读取或格式错误；仍可阅读已有文档")
	} else if name, ok := cfg["project_name"].(string); ok && strings.TrimSpace(name) != "" {
		info.Name = name
	} else {
		info.Warnings = append(info.Warnings, "project.json 未填写 project_name，使用目录名")
	}
	for _, path := range []string{"README.md", "AGENTS.md", "PLAN.md", "progress.md", "docs/task_plan.md", "docs/findings.md", "docs/specs", "docs/reviews", "docs/standards"} {
		st, err := p.safeStat(path)
		dir := filepath.Ext(path) == ""
		if err != nil || dir && !st.IsDir() || !dir && !st.Mode().IsRegular() {
			info.Warnings = append(info.Warnings, "缺少或无法读取："+path)
		}
	}
	visits := 0
	err := fs.WalkDir(p.root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			info.Warnings = append(info.Warnings, "无法读取目录项："+path)
			return nil
		}
		visits++
		if visits > 20000 || len(info.Documents)+len(info.Diagrams) >= maxDocuments {
			info.Warnings = append(info.Warnings, "文件数量超过扫描上限，列表已截断")
			return fs.SkipAll
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || (!strings.Contains(path, "/") && path != "docs") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && allowedDiagram(path) {
			info.Diagrams = append(info.Diagrams, Document{Path: path})
		}
		if d.Type().IsRegular() && allowedDocument(path) {
			info.Documents = append(info.Documents, Document{Path: path})
		}
		return nil
	})
	if err != nil {
		return Info{}, err
	}
	sort.Slice(info.Documents, func(i, j int) bool { return info.Documents[i].Path < info.Documents[j].Path })
	return info, nil
}

func allowedDiagram(path string) bool {
	return fs.ValidPath(path) && !strings.Contains(path, "\\") && strings.HasPrefix(path, "docs/diagrams/") && strings.EqualFold(filepath.Ext(path), ".html")
}

var diagramSourceExts = map[string]bool{".json": true, ".yaml": true, ".yml": true, ".mmd": true, ".puml": true, ".dot": true}

// diagramBaseName 去掉末尾扩展名，用于图与源文件按主名配对。
func diagramBaseName(name string) string { return strings.TrimSuffix(name, filepath.Ext(name)) }

// diagramPairDir 返回项目内斜杠路径的所在目录。
func diagramPairDir(path string) string {
	if cut := strings.LastIndex(path, "/"); cut >= 0 {
		return path[:cut]
	}
	return "."
}

// DiagramSource 返回与交互图 HTML 配对的源文件路径；找不到时返回空字符串。
// 配对规则：同目录、主名等于 HTML 主名或以「<主名>.」为前缀的第一个源扩展名文件，按名称排序取首个。
func (p *Project) DiagramSource(htmlPath string) (string, error) {
	if !allowedDiagram(htmlPath) {
		return "", errors.New("仅允许读取 docs/diagrams 下的 HTML")
	}
	dir := diagramPairDir(htmlPath)
	entries, err := fs.ReadDir(p.root.FS(), dir)
	if err != nil {
		return "", err
	}
	base := diagramBaseName(filepath.Base(htmlPath))
	for _, entry := range entries {
		name := entry.Name()
		if name == filepath.Base(htmlPath) || !entry.Type().IsRegular() || strings.EqualFold(filepath.Ext(name), ".html") || !diagramSourceExts[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		stem := diagramBaseName(name)
		if stem != base && !strings.HasPrefix(stem, base+".") {
			continue
		}
		candidate := name
		if dir != "." {
			candidate = dir + "/" + name
		}
		if _, err := p.safeStat(candidate); err != nil {
			continue
		}
		return candidate, nil
	}
	return "", nil
}

// allowedDiagramSource 判断路径是否为可编辑的交互图源文件：约定目录、源扩展名，且同目录存在配对 HTML。
func (p *Project) allowedDiagramSource(name string) bool {
	if !fs.ValidPath(name) || strings.Contains(name, "\\") || !strings.HasPrefix(name, "docs/diagrams/") || strings.EqualFold(filepath.Ext(name), ".html") || !diagramSourceExts[strings.ToLower(filepath.Ext(name))] {
		return false
	}
	dir := diagramPairDir(name)
	entries, err := fs.ReadDir(p.root.FS(), dir)
	if err != nil {
		return false
	}
	stem := diagramBaseName(filepath.Base(name))
	for _, entry := range entries {
		candidate := entry.Name()
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(candidate), ".html") {
			continue
		}
		htmlStem := diagramBaseName(candidate)
		if stem == htmlStem || strings.HasPrefix(stem, htmlStem+".") {
			return true
		}
	}
	return false
}

// ReadDiagram 只读取约定目录中的独立 HTML，交由隔离 iframe 展示。
func (p *Project) ReadDiagram(path string) (string, error) {
	if !allowedDiagram(path) {
		return "", errors.New("仅允许读取 docs/diagrams 下的 HTML")
	}
	b, err := p.read(path)
	return string(b), err
}

type DocumentContent struct {
	Path     string `json:"path"`
	Markdown string `json:"markdown"`
	Revision string `json:"revision"`
	Editable bool   `json:"editable"`
}

var ErrConflict = errors.New("文件已被其他操作修改，草稿已保留；请核对最新原文后再保存")
var ErrReadOnly = errors.New("文件属于只读区域，不能从网页保存")

func revisionOf(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }
func (p *Project) editable(path string) bool {
	zones := []string{"docs/reviews/discussion"}
	if cfg, err := p.config(); err == nil {
		if raw, ok := cfg["readonly_zones"].(string); ok {
			zones = append(zones, strings.Split(raw, ",")...)
		}
	}
	for _, zone := range zones {
		zone = strings.Trim(strings.TrimSpace(zone), "/")
		if zone != "" && (path == zone || strings.HasPrefix(path, zone+"/")) {
			return false
		}
		// 以文件系统身份识别目录别名，覆盖 macOS 的大小写与规范化路径。
		if zoneInfo, err := p.safeStat(zone); zone != "" && err == nil {
			parts := strings.Split(path, "/")
			for i := range parts {
				if ancestor, e := p.safeStat(strings.Join(parts[:i+1], "/")); e == nil && os.SameFile(zoneInfo, ancestor) {
					return false
				}
			}
		}
	}
	return true
}

// DocumentState 返回正文及用于检测外部改动的版本。
func (p *Project) DocumentState(path string) (DocumentContent, error) {
	body, err := p.ReadDocument(path)
	if err != nil {
		return DocumentContent{}, err
	}
	return DocumentContent{Path: path, Markdown: body, Revision: revisionOf(body), Editable: p.editable(path)}, nil
}

// SaveDocument 检查读取版本后在同目录替换正文，不自动提交 Git。
func (p *Project) SaveDocument(path, markdown, revision string) (DocumentContent, error) {
	if !allowedDocument(path) && !p.allowedDiagramSource(path) {
		return DocumentContent{}, errors.New("无效的 Markdown 或交互图源文件路径")
	}
	if len(markdown) > MaxDocumentBytes || !utf8.ValidString(markdown) {
		return DocumentContent{}, errors.New("正文须为有效 UTF-8，且不超过 2 MiB")
	}
	if !p.editable(path) {
		return DocumentContent{}, ErrReadOnly
	}
	current, err := p.DocumentState(path)
	if err != nil {
		return DocumentContent{}, err
	}
	if revision == "" || current.Revision != revision {
		return DocumentContent{}, ErrConflict
	}
	info, err := p.safeStat(path)
	if err != nil {
		return DocumentContent{}, err
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	parent, err := p.root.OpenRoot(dir)
	if err != nil {
		return DocumentContent{}, err
	}
	defer parent.Close()
	temp := ".workbench-" + rand.Text() + ".tmp"
	f, err := parent.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return DocumentContent{}, err
	}
	defer parent.Remove(temp)
	if err = f.Chmod(info.Mode().Perm()); err != nil {
		f.Close()
		return DocumentContent{}, err
	}
	if _, err = f.WriteString(markdown); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return DocumentContent{}, err
	}
	if closeErr != nil {
		return DocumentContent{}, closeErr
	}
	// 保存准备期间再次核对外部编辑；不同进程仍需通过 Git 和人工 diff 协作。
	latest, err := p.DocumentState(path)
	if err != nil {
		return DocumentContent{}, err
	}
	if latest.Revision != revision {
		return DocumentContent{}, ErrConflict
	}
	if err = parent.Rename(temp, filepath.Base(path)); err != nil {
		return DocumentContent{}, err
	}
	return DocumentContent{Path: path, Markdown: markdown, Revision: revisionOf(markdown), Editable: true}, nil
}
