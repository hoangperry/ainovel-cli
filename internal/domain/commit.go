package domain

// CommitStage biểu thị giai đoạn hiện tại của Saga commit chương.
type CommitStage string

const (
	CommitStageStarted        CommitStage = "started"
	CommitStageStateApplied   CommitStage = "state_applied"
	CommitStageProgressMarked CommitStage = "progress_marked"
	CommitStageSignalSaved    CommitStage = "signal_saved"
)

// PendingCommit ghi lại thông tin khôi phục khi commit chương bị gián đoạn.
type PendingCommit struct {
	Chapter        int           `json:"chapter"`
	Stage          CommitStage   `json:"stage"`
	Summary        string        `json:"summary,omitempty"`
	HookType       string        `json:"hook_type,omitempty"`
	DominantStrand string        `json:"dominant_strand,omitempty"`
	Result         *CommitResult `json:"result,omitempty"`
	StartedAt      string        `json:"started_at,omitempty"`
	UpdatedAt      string        `json:"updated_at,omitempty"`
}
