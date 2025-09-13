package contract

import (
	"path"
)

// NormalizeFileID 规范化路径，统一为跨平台稳定的 FileID。
// 规则：
// - 使用正斜杠分隔符
// - 清理多余分隔符与路径片段（.、..）
// - 保留相对/绝对语义，不做隐式绝对化
func NormalizeFileID(p string) FileID {
	// 手动将所有反斜杠转为正斜杠，确保跨平台一致性
	s := ""
	for _, r := range p {
		if r == '\\' {
			s += "/"
		} else {
			s += string(r)
		}
	}
	// path.Clean 在 POSIX 语义下清理路径（此处已是 '/'' 分隔）
	s = path.Clean(s)
	return FileID(s)
}
