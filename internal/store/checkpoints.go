package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
)

const checkpointsFile = "meta/checkpoints.jsonl"

// CheckpointStore quản lý việc thêm và truy vấn checkpoint cấp step.
// Định dạng đĩa: meta/checkpoints.jsonl, chỉ thêm; truy vấn qua bản sao trong bộ nhớ.
// Bất biến: cache là bản sao của checkpoints.jsonl, được Append/Reset duy trì tại một điểm duy nhất.
// Đồng thời: cache được io.mu bảo vệ, ghi dùng Lock, đọc dùng RLock.
type CheckpointStore struct {
	io     *IO
	seqGen atomic.Int64
	cache  []domain.Checkpoint
}

// NewCheckpointStore tạo store checkpoint, nạp một lần các checkpoint hiện có từ đĩa vào cache.
func NewCheckpointStore(io *IO) *CheckpointStore {
	cs := &CheckpointStore{io: io}
	cs.loadFromDisk()
	return cs
}

// loadFromDisk đọc một lần jsonl trên đĩa vào cache và khôi phục seqGen.
func (cs *CheckpointStore) loadFromDisk() {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()

	cs.cache = readCheckpointsFile(cs.io.path(checkpointsFile))
	var maxSeq int64
	for _, cp := range cs.cache {
		if cp.Seq > maxSeq {
			maxSeq = cp.Seq
		}
	}
	cs.seqGen.Store(maxSeq)
}

// Append thêm một checkpoint.
// Idempotent: nếu cùng Scope + Step + Digest đã tồn tại thì bỏ qua ghi, trả về bản ghi sẵn có.
func (cs *CheckpointStore) Append(scope domain.Scope, step, artifact, digest string) (*domain.Checkpoint, error) {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()

	if digest != "" {
		for i := len(cs.cache) - 1; i >= 0; i-- {
			cp := cs.cache[i]
			if cp.Scope.Matches(scope) && cp.Step == step && cp.Digest == digest {
				return &cp, nil
			}
		}
	}

	// chỉ tăng seq sau khi ghi thành công, tránh ghi lỗi để lại lỗ hổng số thứ tự vĩnh viễn.
	// đã giữ khóa ghi io.mu, giữa Load+Store sẽ không bị tranh chấp đồng thời.
	seq := cs.seqGen.Load() + 1
	cp := domain.Checkpoint{
		Seq:        seq,
		Scope:      scope,
		Step:       step,
		Artifact:   artifact,
		Digest:     digest,
		OccurredAt: time.Now(),
	}

	data, err := json.Marshal(cp)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := cs.io.AppendLineUnlocked(checkpointsFile, data); err != nil {
		return nil, err
	}
	cs.seqGen.Store(seq)
	cs.cache = append(cs.cache, cp)
	return &cp, nil
}

// AppendArtifact tính vân tay nội dung artifact rồi thêm checkpoint.
func (cs *CheckpointStore) AppendArtifact(scope domain.Scope, step, artifact string) (*domain.Checkpoint, error) {
	if artifact == "" {
		return cs.Append(scope, step, "", "")
	}
	data, err := cs.io.ReadFile(artifact)
	if err != nil {
		return nil, fmt.Errorf("digest artifact %s: %w", artifact, err)
	}
	sum := sha256.Sum256(data)
	return cs.Append(scope, step, artifact, "sha256:"+hex.EncodeToString(sum[:]))
}

// Latest trả về checkpoint mới nhất của scope chỉ định.
func (cs *CheckpointStore) Latest(scope domain.Scope) *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	for i := len(cs.cache) - 1; i >= 0; i-- {
		if cs.cache[i].Scope.Matches(scope) {
			cp := cs.cache[i]
			return &cp
		}
	}
	return nil
}

// LatestByStep trả về checkpoint mới nhất của scope + step chỉ định.
func (cs *CheckpointStore) LatestByStep(scope domain.Scope, step string) *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	for i := len(cs.cache) - 1; i >= 0; i-- {
		cp := cs.cache[i]
		if cp.Scope.Matches(scope) && cp.Step == step {
			return &cp
		}
	}
	return nil
}

// LatestGlobal trả về checkpoint mới nhất toàn cục (không phân biệt scope).
func (cs *CheckpointStore) LatestGlobal() *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	if len(cs.cache) == 0 {
		return nil
	}
	cp := cs.cache[len(cs.cache)-1]
	return &cp
}

// All trả về bản sao danh sách toàn bộ checkpoint (theo seq tăng dần).
func (cs *CheckpointStore) All() []domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	if len(cs.cache) == 0 {
		return nil
	}
	out := make([]domain.Checkpoint, len(cs.cache))
	copy(out, cs.cache)
	return out
}

// Reset xóa sạch file checkpoint và cache. Chỉ dùng khi tạo tiểu thuyết mới.
// Xóa file trước rồi mới xóa bộ nhớ: khi xóa lỗi vẫn giữ cache và seqGen, tránh lệch trạng thái giữa bộ nhớ và đĩa.
func (cs *CheckpointStore) Reset() error {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()
	if err := cs.io.RemoveFileUnlocked(checkpointsFile); err != nil {
		return err
	}
	cs.seqGen.Store(0)
	cs.cache = nil
	return nil
}

// readCheckpointsFile phân tích jsonl; bỏ qua dòng sai định dạng để dung thứ việc cắt cụt ở cuối.
func readCheckpointsFile(path string) []domain.Checkpoint {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var result []domain.Checkpoint
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var cp domain.Checkpoint
		if json.Unmarshal(line, &cp) == nil {
			result = append(result, cp)
		}
	}
	return result
}
