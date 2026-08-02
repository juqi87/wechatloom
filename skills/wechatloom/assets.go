package wechatloomskill

import "embed"

// Assets is the portable Codex skill bundled into every WeChatLoom binary.
//
//go:embed SKILL.md agents/openai.yaml
var Assets embed.FS

var Files = []string{"SKILL.md", "agents/openai.yaml"}
