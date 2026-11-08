package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var (
	upPattern   = regexp.MustCompile(`^V(\d+(?:\.\d+)*)__(.+)\.sql$`)
	downPattern = regexp.MustCompile(`^U(\d+(?:\.\d+)*)__(.+)\.sql$`)
)

// pending 是按版本聚合的中间态：V 给正向上行与描述，U 只补回滚。
type pending struct {
	description string
	up          string
	down        string
	hasUp       bool
}

// Scan 扫描迁移目录，装配成按版本升序的迁移单元。
//
// 命名约定：V<版本>__<描述>.sql 为正向，U<版本>__<描述>.sql 为回滚。
// 命名不合约定直接报错，不做猜测——猜错的代价是把回滚脚本当正向执行。
func Scan(directory string) ([]File, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, newError(CodeMigrationsDirMissing, "迁移目录不存在: %s", directory)
		}
		return nil, fmt.Errorf("读取迁移目录失败: %w", err)
	}

	collected := make(map[string]*pending)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		up := upPattern.FindStringSubmatch(name)
		down := downPattern.FindStringSubmatch(name)

		switch {
		case up != nil:
			content, err := readFile(filepath.Join(directory, name))
			if err != nil {
				return nil, err
			}
			item := ensure(collected, up[1])
			item.description = up[2]
			item.up = content
			item.hasUp = true
		case down != nil:
			content, err := readFile(filepath.Join(directory, name))
			if err != nil {
				return nil, err
			}
			item := ensure(collected, down[1])
			if item.description == "" {
				item.description = down[2]
			}
			item.down = content
		case filepath.Ext(name) == ".sql":
			return nil, newError(CodeBadMigrationName,
				"迁移文件命名不合约定（应为 V<版本>__<描述>.sql）: %s", name)
		}
	}

	files := make([]File, 0, len(collected))
	for version, item := range collected {
		if !item.hasUp {
			return nil, newError(CodeMissingUpScript, "版本 %s 只有回滚脚本，没有正向脚本", version)
		}
		files = append(files, File{
			Version:     version,
			Description: item.description,
			UpSQL:       item.up,
			DownSQL:     item.down,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return CompareVersions(files[i].Version, files[j].Version) < 0
	})
	return files, nil
}

func ensure(collected map[string]*pending, version string) *pending {
	item, ok := collected[version]
	if !ok {
		item = &pending{}
		collected[version] = item
	}
	return item
}

func readFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取迁移文件失败 %s: %w", path, err)
	}
	return string(content), nil
}
