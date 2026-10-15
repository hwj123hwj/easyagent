package runtime

import "errors"

// ErrToolsUpdating means admission failed before the message was executed.
var ErrToolsUpdating = errors.New("工具配置正在更新，请稍后重试；消息尚未执行")
