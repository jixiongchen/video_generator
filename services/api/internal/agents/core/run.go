package core

import "encoding/json"

// Run 是持久化任务快照。Sequence 单调增长，可直接作为 SSE 的恢复序号。
// Steps 只记录检查点，不重复保存整本原文；较大产物独立按哈希保存。
type Run struct {
	ID            string          `json:"id"`
	RequestID     string          `json:"requestId,omitempty"`
	Agent         string          `json:"agent"`
	ProjectID     string          `json:"projectId"`
	Stage         string          `json:"stage"`
	Status        string          `json:"status"`
	Sequence      int             `json:"sequence"`
	Current       string          `json:"current"`
	Completed     int             `json:"completed"`
	Steps         map[string]Step `json:"steps"`
	Targets       []string        `json:"targets"`
	Instruction   string          `json:"instruction"`
	SceneID       string          `json:"sceneId,omitempty"`
	InputRevision int             `json:"inputRevision"`
	Model         string          `json:"model"`
	Protocol      string          `json:"protocol"`
	Error         string          `json:"error,omitempty"`
	CreatedAt     string          `json:"createdAt"`
	UpdatedAt     string          `json:"updatedAt"`
}

type Step struct {
	Key       string          `json:"key"`
	Operation string          `json:"operation"`
	Usage     json.RawMessage `json:"usage"`
}

// Active 包含 pausing：暂停在当前调用完成并保存之后生效，不丢弃已付费的结果。
func Active(status string) bool { return status == "running" || status == "pausing" }
