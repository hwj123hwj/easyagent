package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DefaultMaxOutputLen 是工具输出的默认字符上限（50KB）。对齐 pi 的
// DEFAULT_MAX_BYTES：read/bash/grep 的自带截断都用它，编码场景下 30000
// 偏紧，模型经常要为看完整文件多绕几轮。
const DefaultMaxOutputLen = 51200

// TruncateOutput truncates output to maxLen characters.
// It keeps the front 80% and back 20%, inserting a truncation marker in between.
// If maxLen <= 0, DefaultMaxOutputLen is used.
// If output fits within maxLen, it is returned unchanged.
func TruncateOutput(output string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = DefaultMaxOutputLen
	}
	if len(output) <= maxLen {
		return output
	}

	frontLen := int(float64(maxLen) * 0.8)
	backLen := maxLen - frontLen

	truncated := len(output) - maxLen
	marker := fmt.Sprintf("\n... [truncated %d characters] ...\n", truncated)

	var b strings.Builder
	b.Grow(maxLen + len(marker))
	b.WriteString(output[:frontLen])
	b.WriteString(marker)
	b.WriteString(output[len(output)-backLen:])
	return b.String()
}

// truncateByteCap 在不超过 max 字节的前提下截断 s，且不切开 UTF-8 多字节字符。
func truncateByteCap(s string, max int) string {
	if max < 0 {
		max = 0
	}
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
