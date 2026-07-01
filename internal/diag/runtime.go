package diag

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

const (
	logTailCap   = 200 << 10 // Log chỉ lấy phần đuôi 200KB (vòng lặp là hiện tượng cận điểm)
	sessionTail  = 80        // Số dòng phần đuôi khung (để xem thứ tự trước sau của việc điều phối)
	repeatWindow = 150       // Tổng hợp trùng lặp chỉ nhìn ngần này sự kiện cận điểm — chạy dài thì tool bình thường tích luỹ cả trăm lần,
	// vòng lặp thật là tập trung cao độ ở cận điểm; dùng cửa sổ thay vì tích luỹ, tránh phán nhầm "tiến triển bình thường" thành "lặp chết".
	recentAgents = 2  // Số session subagent hoạt động gần nhất quét thêm
	repeatMin    = 3  // Trùng lặp đạt mấy lần mới tính là "tín hiệu tần suất cao"
	repeatTopN   = 12 // Vân tay trùng lặp liệt kê tối đa mấy dòng
)

// RuntimeCapture là kết quả khử nhạy cảm của một lần capture trạng thái chạy. Chỉ chứa tín hiệu trạng thái chạy;
// trạng thái sáng tác như phase/flow/chương do Report.Stats mang, không lặp lại ở đây.
type RuntimeCapture struct {
	GoOS, GoArch  string
	Models        []RoleModel  // provider/model thực tế có hiệu lực của mỗi session (thu từ _meta)
	CurrentStep   string       // checkpoint mới nhất: scope.step
	StuckStep     string       // Phần đuôi liên tiếp cùng step; "" = không kẹt
	StuckCount    int          // Số lần liên tiếp
	Repeats       []RepeatStat // Vân tay trùng lặp top-N (tín hiệu vòng lặp)
	DupContent    []DupStat    // Văn bản cùng sha xuất hiện lặp đi lặp lại (sinh lặp cùng một đoạn)
	LogKinds      map[string]int
	LogErrors     int
	LogWarns      int
	StopGuard     int
	Tail          []SkelEvent // N khung cuối (để xem thứ tự)
	RedactedTexts int         // Tổng số khối văn bản đã che (tự kiểm khử nhạy cảm)
	Sources       []string    // Nguồn thực tế đọc được (tự kiểm)
}

// RoleModel ghi lại provider/model một session thực sự dùng.
type RoleModel struct {
	Agent, Provider, Model string
}

// RepeatStat là một vân tay trùng lặp kèm số lần.
type RepeatStat struct {
	Sig   string
	Count int
}

// DupStat là số lần một đoạn văn bản đã khử nhạy cảm xuất hiện lặp lại.
type DupStat struct {
	Sha   string
	Count int
}

// sessionLine phân giải một dòng của sessions/*.jsonl: agentcore.Message nhúng + _meta tuỳ chọn.
type sessionLine struct {
	agentcore.Message
	Meta *struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"_meta"`
}

var kindRe = regexp.MustCompile(`kind=(\S+)`)

// CaptureRuntime đọc-chỉ-đọc các tín hiệu trạng thái chạy từ thư mục output rồi tổng hợp đã khử nhạy cảm.
// Nguồn nào thiếu cũng hạ cấp an toàn (không báo lỗi), best-effort.
func CaptureRuntime(s *store.Store) RuntimeCapture {
	rc := RuntimeCapture{GoOS: runtime.GOOS, GoArch: runtime.GOARCH, LogKinds: map[string]int{}}

	rc.CurrentStep, rc.StuckStep, rc.StuckCount = analyzeCheckpoints(s.Checkpoints.All())
	captureSessions(s.Dir(), &rc)
	captureLog(s.Dir(), &rc)
	return rc
}

// analyzeCheckpoints lấy step mới nhất, và tính phần đuôi liên tiếp cùng step (tín hiệu kẹt).
func analyzeCheckpoints(cps []domain.Checkpoint) (current, stuck string, count int) {
	if len(cps) == 0 {
		return "", "", 0
	}
	key := func(c domain.Checkpoint) string { return fmt.Sprintf("%s.%s", c.Scope, c.Step) }
	current = key(cps[len(cps)-1])
	n := 1
	for i := len(cps) - 2; i >= 0; i-- {
		if key(cps[i]) == current {
			n++
		} else {
			break
		}
	}
	if n >= repeatMin {
		stuck, count = current, n
	}
	return current, stuck, count
}

// captureSessions quét coordinator + session subagent gần nhất, tổng hợp đã khử nhạy cảm.
func captureSessions(dir string, rc *RuntimeCapture) {
	sessDir := filepath.Join(dir, "meta", "sessions")
	files := sessionFiles(sessDir)

	repeats := map[string]int{}
	dups := map[string]int{}
	models := map[string]RoleModel{}

	for _, f := range files {
		evs := scanSession(filepath.Join(sessDir, f.path), f.agent, rc, models)
		// Tổng hợp chỉ nhìn cửa sổ cận điểm: chạy dài thì subagent/novel_context tích luỹ cả trăm lần là tiến triển bình thường,
		// không phải vòng lặp; lặp chết thật là tập trung cao độ ở cận điểm.
		aggregateRepeats(f.agent, tailEvents(evs, repeatWindow), repeats, dups)
		// Phần đuôi khung ưu tiên lấy coordinator — vòng lặp điều phối nhìn rõ nhất ở đây.
		if f.agent == "coordinator" && len(evs) > 0 {
			rc.Tail = tailEvents(evs, sessionTail)
		}
		rc.Sources = append(rc.Sources, "sessions/"+f.path)
	}
	if len(rc.Tail) == 0 {
		// Không có session coordinator thì lùi về một subagent gần nhất.
		for _, f := range files {
			if evs := scanSessionTailOnly(filepath.Join(sessDir, f.path), f.agent); len(evs) > 0 {
				rc.Tail = tailEvents(evs, sessionTail)
				break
			}
		}
	}

	rc.Repeats = topRepeats(repeats)
	rc.DupContent = topDups(dups)
	rc.Models = sortedModels(models)
}

type sessionFile struct {
	path  string // tương đối sessDir
	agent string
}

// sessionFiles trả về coordinator.jsonl + các session subagent hoạt động gần nhất.
func sessionFiles(sessDir string) []sessionFile {
	var out []sessionFile
	if _, err := os.Stat(filepath.Join(sessDir, "coordinator.jsonl")); err == nil {
		out = append(out, sessionFile{path: "coordinator.jsonl", agent: "coordinator"})
	}

	agentsDir := filepath.Join(sessDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return out
	}
	type withTime struct {
		name string
		mod  int64
	}
	var agents []withTime
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil {
			agents = append(agents, withTime{e.Name(), info.ModTime().UnixNano()})
		}
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].mod > agents[j].mod })
	for i, a := range agents {
		if i >= recentAgents {
			break
		}
		stem := strings.TrimSuffix(a.name, ".jsonl")
		out = append(out, sessionFile{path: filepath.Join("agents", a.name), agent: stem})
	}
	return out
}

// scanSession đọc một file session, khử nhạy cảm từng dòng, thu chuỗi sự kiện và model per-agent.
// Tổng hợp trùng lặp/cùng đoạn không làm ở đây — giao cho aggregateRepeats tính trên cửa sổ cận điểm.
func scanSession(path, agent string, rc *RuntimeCapture, models map[string]RoleModel) []SkelEvent {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var evs []SkelEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var sl sessionLine
		if json.Unmarshal(sc.Bytes(), &sl) != nil {
			continue
		}
		ev := redactMessage(agent, sl.Message)
		evs = append(evs, ev)
		rc.RedactedTexts += ev.Redacted
		if sl.Meta != nil && (sl.Meta.Provider != "" || sl.Meta.Model != "") {
			models[agent] = RoleModel{Agent: agent, Provider: sl.Meta.Provider, Model: sl.Meta.Model}
		}
	}
	return evs
}

// aggregateRepeats tích luỹ vân tay trùng lặp và văn bản cùng đoạn trên cửa sổ sự kiện cho trước.
func aggregateRepeats(agent string, evs []SkelEvent, repeats, dups map[string]int) {
	for _, ev := range evs {
		for _, t := range ev.Tools {
			sig := agent + " · " + t.Name
			if t.Invalid {
				sig += " (args invalid)"
			}
			repeats[sig]++
		}
		if ev.ErrClass != "" {
			repeats[agent+" · err: "+ev.ErrClass]++
		}
		if ev.TextSha != "" {
			dups[ev.TextSha]++
		}
	}
}

// scanSessionTailOnly chỉ lấy khung (không tính tổng hợp), dùng cho phần đuôi dự phòng khi thiếu coordinator.
func scanSessionTailOnly(path, agent string) []SkelEvent {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var evs []SkelEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var sl sessionLine
		if json.Unmarshal(sc.Bytes(), &sl) != nil {
			continue
		}
		evs = append(evs, redactMessage(agent, sl.Message))
	}
	return evs
}

func tailEvents(evs []SkelEvent, n int) []SkelEvent {
	if len(evs) <= n {
		return evs
	}
	return evs[len(evs)-n:]
}

// captureLog đọc phần đuôi log, chỉ tổng hợp tín hiệu cấu trúc (kind/error/warn/stop_guard),
// không đưa dòng log gốc vào gói — Detail có thể lẫn chính văn.
func captureLog(dir string, rc *RuntimeCapture) {
	path := filepath.Join(dir, "logs", "tui.log")
	tail, ok := readTail(path)
	if !ok {
		path = filepath.Join(dir, "logs", "headless.log")
		tail, ok = readTail(path)
	}
	if !ok {
		return
	}
	rc.Sources = append(rc.Sources, "logs/"+filepath.Base(path)+contentlang.Pick(" (尾部)", " (phần đuôi)"))

	sc := bufio.NewScanner(bytes.NewReader(tail))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "level=ERROR"):
			rc.LogErrors++
		case strings.Contains(line, "level=WARN"):
			rc.LogWarns++
		}
		if m := kindRe.FindStringSubmatch(line); m != nil {
			rc.LogKinds[m[1]]++
		}
		if strings.Contains(line, "stop_guard") {
			rc.StopGuard++
		}
	}
}

// readTail đọc logTailCap byte cuối file, và bỏ nửa dòng đầu có thể bị cắt cụt.
func readTail(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false
	}
	size := info.Size()
	var off int64
	if size > logTailCap {
		off = size - logTailCap
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, false
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false
	}
	if off > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return data, true
}

func topRepeats(m map[string]int) []RepeatStat {
	var out []RepeatStat
	for sig, c := range m {
		if c >= repeatMin {
			out = append(out, RepeatStat{Sig: sig, Count: c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Sig < out[j].Sig
	})
	if len(out) > repeatTopN {
		out = out[:repeatTopN]
	}
	return out
}

func topDups(m map[string]int) []DupStat {
	var out []DupStat
	for sha, c := range m {
		if c >= repeatMin {
			out = append(out, DupStat{Sha: sha, Count: c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Sha < out[j].Sha
	})
	return out
}

func sortedModels(m map[string]RoleModel) []RoleModel {
	out := make([]RoleModel, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}
