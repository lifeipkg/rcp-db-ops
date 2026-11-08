// Package migration 是版本化 DDL 的执行器：扫描、按序应用、记账、回滚。
//
// 迁移脚本真源在部署仓 rcp-ops/migrations（RL-004），本包只执行与记账。
package migration

import "fmt"

// 错误码：客户端按 code 区分失败原因，message 只用于人读。
const (
	CodeMigrationsDirMissing = "MIGRATIONS_DIR_MISSING"
	CodeBadMigrationName     = "BAD_MIGRATION_NAME"
	CodeMissingUpScript      = "MISSING_UP_SCRIPT"
	CodeMigrationFailed      = "MIGRATION_FAILED"
	CodeIrreversible         = "IRREVERSIBLE_MIGRATION"
	CodeNothingToRollback    = "NOTHING_TO_ROLLBACK"
)

// Error 是迁移领域的可预期失败，对应 HTTP 400 + 编码化原因。
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Err }

func newError(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
