package migration

import (
	"strconv"
	"strings"
)

// CompareVersions 按数字段比较版本号：1.10.0 > 1.9.0，缺失段按 0 处理。
//
// 不能用字符串比较：`1.10` < `1.9`，会让迁移顺序错乱，而迁移顺序错乱是不可回滚的
// 事故。文件名正则已保证版本只含数字与点，故 Atoi 不会失败。
func CompareVersions(left, right string) int {
	a := strings.Split(left, ".")
	b := strings.Split(right, ".")
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	for i := 0; i < length; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av, _ = strconv.Atoi(a[i])
		}
		if i < len(b) {
			bv, _ = strconv.Atoi(b[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// maxVersion 取版本集合里的最大值；空集合返回空串。
func maxVersion(versions []string) string {
	if len(versions) == 0 {
		return ""
	}
	max := versions[0]
	for _, version := range versions[1:] {
		if CompareVersions(version, max) > 0 {
			max = version
		}
	}
	return max
}
