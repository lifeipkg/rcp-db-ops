package migration

import "strings"

// File 是一个版本化迁移单元。Up 必填，Down 可缺省——缺省表示不可回滚，
// 回滚时明确报 IRREVERSIBLE_MIGRATION，而不是静默跳过留下不一致的表。
type File struct {
	Version     string
	Description string
	UpSQL       string
	DownSQL     string
}

// Reversible 判断该版本是否提供了回滚脚本。
func (f File) Reversible() bool {
	return strings.TrimSpace(f.DownSQL) != ""
}

// SplitStatements 按分号切分脚本，丢弃空白片段。
//
// 已知边界：字符串字面量里出现分号会被误切。迁移脚本由平台维护、内容可审，
// 这里不为罕见形态引入 SQL 解析器；真的需要时先把脚本改写成不含字面量分号的形式。
func SplitStatements(script string) []string {
	parts := strings.Split(script, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			statements = append(statements, trimmed)
		}
	}
	return statements
}
