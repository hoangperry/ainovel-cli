package diag

import "testing"

// TestRuntimeFindings_Classify chứng minh vân tay trùng lặp được phân loại theo hình thái, nâng/hạ cấp theo ngưỡng đúng,
// và Finding trạng thái chạy đều AutoNone (kỷ luật quan sát viên: chỉ chẩn đoán không sinh Action).
func TestRuntimeFindings_Classify(t *testing.T) {
	rc := RuntimeCapture{
		Repeats: []RepeatStat{
			{Sig: "coordinator · err: InputValidationError", Count: 14}, // Vòng lặp lỗi critical
			{Sig: "coordinator · subagent", Count: 45},                  // Tool tần suất cao bình thường → không sinh Finding
			{Sig: "writer · save_plan (args invalid)", Count: 4},        // Tham số không hợp lệ warning
		},
		StuckStep:  "writing.commit_ch07",
		StuckCount: 9, // Kẹt critical
		LogKinds:   map[string]int{"stream_idle": 4},
		LogErrors:  270, // Tích luỹ chạy dài, không nên sinh Finding riêng
	}

	fs := runtimeFindings(&rc)
	sev := map[string]Severity{}
	for _, f := range fs {
		sev[f.Rule] = f.Severity
		if f.AutoLevel != AutoNone {
			t.Errorf("%s 应为 AutoNone（观察者纪律），got %s", f.Rule, f.AutoLevel)
		}
	}

	want := map[string]Severity{
		"RepeatedToolError": SevCritical,
		"ArgsInvalidLoop":   SevWarning,
		"StuckStep":         SevCritical,
		"StreamIdleStorm":   SevWarning,
	}
	for rule, w := range want {
		if sev[rule] != w {
			t.Errorf("%s: got %q want %q", rule, sev[rule], w)
		}
	}
	// Tool tần suất cao bình thường / error tích luỹ trong log không nên sinh Finding (tránh báo nhầm khi chạy dài).
	if _, ok := sev["RepeatedToolCall"]; ok {
		t.Error("普通工具重复不应产 Finding")
	}
	if _, ok := sev["LogErrorBurst"]; ok {
		t.Error("日志 error 累计不应单独产 Finding")
	}
}

// TestRuntimeFindings_Quiet chứng minh khi không có tín hiệu bất thường thì không sinh Finding trạng thái chạy nào (zero báo nhầm).
func TestRuntimeFindings_Quiet(t *testing.T) {
	rc := RuntimeCapture{
		LogKinds:  map[string]int{"stream_idle": 1}, // Dưới ngưỡng
		LogErrors: 2,
	}
	if fs := runtimeFindings(&rc); len(fs) != 0 {
		t.Errorf("安静态不应产 Finding，got %d: %+v", len(fs), fs)
	}
}
