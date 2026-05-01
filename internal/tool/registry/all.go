// Package registry imports all tool sub-packages to trigger their init() registration.
package registry

import (
	_ "github.com/genai-io/gen-code/internal/tool/agent"
	_ "github.com/genai-io/gen-code/internal/tool/cron"
	_ "github.com/genai-io/gen-code/internal/tool/fs"
	_ "github.com/genai-io/gen-code/internal/tool/mode"
	_ "github.com/genai-io/gen-code/internal/tool/skill"
	_ "github.com/genai-io/gen-code/internal/tool/task"
	_ "github.com/genai-io/gen-code/internal/tool/tasktools"
	_ "github.com/genai-io/gen-code/internal/tool/web"
)
