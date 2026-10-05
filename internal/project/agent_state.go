package project

import (
	"errors"
	"io/fs"
	"os"
)

// OpenSessionStorage 在明确创建会话时才建立状态目录；读取不写入文件。
func (p *Project) OpenSessionStorage(create bool) (*os.Root, error) {
	for _, path := range []string{".agent-state", ".agent-state/sessions"} {
		info, err := p.safeStat(path)
		if errors.Is(err, fs.ErrNotExist) && create {
			if err = p.root.Mkdir(path, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
				return nil, err
			}
			info, err = p.safeStat(path)
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, errors.New("会话状态路径不是目录")
		}
	}
	return p.root.OpenRoot(".agent-state/sessions")
}
