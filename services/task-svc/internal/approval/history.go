package approval

import (
	"sort"
	"time"
)

// 历史动作常量。
const (
	HistorySubmit      = "submit"       // 提交审批请求
	HistoryApprove     = "approve"      // 同意
	HistoryReject      = "reject"       // 拒绝
	HistoryCancel      = "cancel"       // 取消
	HistoryTimeout     = "timeout"      // 超时
	HistoryStepAdvance = "step_advance" // 步骤推进
	HistoryFlowCreated = "flow_created" // 流创建（用于流历史，本包暂不持久化流历史）
)

// HistoryEntry 历史时间线条目。
type HistoryEntry struct {
	Timestamp time.Time // 发生时间
	Action    string    // 动作类型（HistorySubmit/Approve/Reject/Cancel/Timeout/StepAdvance）
	UserID    string    // 操作人 userID（submit=发起人，approve/reject=审批人，timeout=空）
	StepID    string    // 关联步骤 ID（无步骤则为空）
	Comment   string    // 备注
}

// History 审批请求的完整历史时间线。
type History struct {
	RequestID string         // 请求 ID
	Timeline  []HistoryEntry // 按时间升序排列
}

// Append 追加一条历史条目并保持时间升序。
// 调用方应已持有外部锁（若 History 共享）。
func (h *History) Append(entry HistoryEntry) {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	h.Timeline = append(h.Timeline, entry)
	sort.SliceStable(h.Timeline, func(i, j int) bool {
		return h.Timeline[i].Timestamp.Before(h.Timeline[j].Timestamp)
	})
}

// Last 返回最近一条历史条目；空则返回零值与 false。
func (h *History) Last() (HistoryEntry, bool) {
	if len(h.Timeline) == 0 {
		return HistoryEntry{}, false
	}
	return h.Timeline[len(h.Timeline)-1], true
}

// FilterByAction 返回指定动作类型的子集（保持顺序）。
func (h *History) FilterByAction(action string) []HistoryEntry {
	out := make([]HistoryEntry, 0, len(h.Timeline))
	for i := range h.Timeline {
		if h.Timeline[i].Action == action {
			out = append(out, h.Timeline[i])
		}
	}
	return out
}
