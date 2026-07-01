package host

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/voocel/agentcore"
)

// sessionRecord là dạng parse nhẹ của một record trong meta/sessions/*.jsonl — chỉ lấy
// các field mà usage tích lũy cần. Các field lớn như Content thì bỏ qua parse, tiết kiệm IO giai đoạn khởi động.
//
// Quy gán model ba bậc hạ cấp:
//  1. Usage.Provider/Model — model response thật do agentcore/litellm truyền qua (ưu tiên)
//  2. Meta(_meta)          — khi thượng nguồn không truyền qua, phía ghi do ModelLookup bù model "đang có hiệu lực lúc đó"
//  3. Cả hai đều không có   — replay lui về effectiveModel dùng ModelSet hiện tại suy ngược (độ chính xác bị tổn hại)
type sessionRecord struct {
	Role  agentcore.Role     `json:"role"`
	Usage *agentcore.Usage   `json:"usage,omitempty"`
	Meta  *sessionRecordMeta `json:"_meta,omitempty"`
}

type sessionRecordMeta struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// ReplaySessions quét meta/sessions/coordinator.jsonl và meta/sessions/agents/*.jsonl,
// cộng dồn lại usage của từng tin nhắn assistant vào tracker. Trả về số record backfill.
//
// Ràng buộc gọi: chỉ gọi một lần khi meta/usage.json thiếu (lần đầu nâng cấp hoặc schema thay đổi), để
// backfill dữ liệu lịch sử. Persist hằng ngày đi qua SaveNow / autoSaveLoop.
//
// Phụ thuộc độ chính xác xem ba bậc hạ cấp ở ghi chú sessionRecord — bậc 3 (cả Usage và _meta đều thiếu)
// chỉ kích hoạt với log cũ hơn hoặc khi thượng nguồn bất thường.
func (t *UsageTracker) ReplaySessions(rootDir string) (int, error) {
	if t == nil {
		return 0, nil
	}
	sessionsDir := filepath.Join(rootDir, "meta", "sessions")
	info, err := os.Stat(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}

	total := 0
	if n, err := t.replayFile(filepath.Join(sessionsDir, "coordinator.jsonl"), "coordinator"); err != nil {
		slog.Warn("replay coordinator session failed", "module", "usage", "err", err)
	} else {
		total += n
	}

	agentsDir := filepath.Join(sessionsDir, "agents")
	walkErr := filepath.WalkDir(agentsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		agentName := parseAgentNameFromFile(name)
		if agentName == "" {
			return nil
		}
		n, fileErr := t.replayFile(path, agentName)
		if fileErr != nil {
			slog.Warn("replay agent session failed", "module", "usage", "file", name, "err", fileErr)
			return nil
		}
		total += n
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return total, walkErr
	}
	return total, nil
}

// replayFile quét một file jsonl, đưa mọi tin nhắn assistant có Usage cho accumulate.
// agentName do bên gọi truyền vào (coordinator hoặc tên sub-agent parse từ tên file).
func (t *UsageTracker) replayFile(path, agentName string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	role := agentRoleName(agentName)
	count := 0
	scanner := bufio.NewScanner(f)
	// Một dòng có thể rất dài (tin nhắn assistant + tool args ... đều bị làm phẳng), nới lên 4MB.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec sessionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Role != agentcore.RoleAssistant || rec.Usage == nil {
			continue
		}
		provider, modelName := usageActualModel(rec.Usage)
		if rec.Meta != nil {
			if provider == "" {
				provider = rec.Meta.Provider
			}
			if modelName == "" {
				modelName = rec.Meta.Model
			}
		}
		t.accumulate(role, provider, modelName, *rec.Usage)
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("scan %s: %w", path, err)
	}
	return count, nil
}

// parseAgentNameFromFile trích tên agent (phần trước "-") từ "writer-ch01.jsonl" / "architect_short-001.jsonl".
// Quy ước đặt tên xem store/session.go::subAgentPath:
// agentName không chứa dash, suffix là ch<n> hoặc số thứ tự tăng dần.
func parseAgentNameFromFile(name string) string {
	base := strings.TrimSuffix(name, ".jsonl")
	if i := strings.Index(base, "-"); i > 0 {
		return base[:i]
	}
	return ""
}
