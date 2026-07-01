package headless

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/tools"
	"github.com/voocel/ainovel-cli/internal/utils"
)

type terminalAskUser struct {
	in  *bufio.Reader
	out io.Writer
	mu  sync.Mutex
}

func newTerminalAskUser(in io.Reader, out io.Writer) *terminalAskUser {
	return &terminalAskUser{
		in:  bufio.NewReader(in),
		out: out,
	}
}

func (h *terminalAskUser) handle(ctx context.Context, questions []tools.Question) (*tools.AskUserResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	resp := &tools.AskUserResponse{
		Answers: make(map[string]string, len(questions)),
		Notes:   make(map[string]string),
	}

	for _, q := range questions {
		answer, note, err := h.askOne(ctx, q)
		if err != nil {
			return nil, err
		}
		resp.Answers[q.Question] = answer
		if strings.TrimSpace(note) != "" {
			resp.Notes[q.Question] = note
		}
	}

	return resp, nil
}

func (h *terminalAskUser) askOne(ctx context.Context, q tools.Question) (string, string, error) {
	fmt.Fprintf(h.out, "\n[%s] %s\n", q.Header, q.Question)
	for i, opt := range q.Options {
		fmt.Fprintf(h.out, "  %d. %s - %s\n", i+1, opt.Label, opt.Description)
	}
	fmt.Fprintln(h.out, contentlang.Pick("  0. 自定义输入", "  0. Nhập tùy chỉnh"))

	for {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		if q.MultiSelect {
			fmt.Fprint(h.out, contentlang.Pick("请输入编号，多个用逗号分隔: ", "Nhập số thứ tự, nhiều lựa chọn cách nhau bằng dấu phẩy: "))
		} else {
			fmt.Fprint(h.out, contentlang.Pick("请输入编号: ", "Nhập số thứ tự: "))
		}

		line, err := h.readLine()
		if err != nil {
			return "", "", err
		}
		line = utils.CleanInputLine(line)
		if line == "" {
			fmt.Fprintln(h.out, contentlang.Pick("输入不能为空，请重试。", "Không được để trống, vui lòng thử lại."))
			continue
		}
		if line == "0" {
			fmt.Fprint(h.out, contentlang.Pick("请输入自定义内容: ", "Nhập nội dung tùy chỉnh: "))
			note, err := h.readLine()
			if err != nil {
				return "", "", err
			}
			note = utils.CleanInputLine(note)
			if note == "" {
				fmt.Fprintln(h.out, contentlang.Pick("自定义内容不能为空，请重试。", "Nội dung tùy chỉnh không được để trống, vui lòng thử lại."))
				continue
			}
			return contentlang.Pick("自定义", "Tùy chỉnh"), note, nil
		}

		labels, err := parseSelections(line, q.Options, q.MultiSelect)
		if err != nil {
			fmt.Fprintln(h.out, contentlang.Pick(
				fmt.Sprintf("%v，请重试。", err),
				fmt.Sprintf("%v, vui lòng thử lại.", err)))
			continue
		}
		return strings.Join(labels, "、"), "", nil
	}
}

func (h *terminalAskUser) readLine() (string, error) {
	line, err := h.in.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func parseSelections(line string, options []tools.Option, multi bool) ([]string, error) {
	parts := strings.Split(line, ",")
	if !multi && len(parts) > 1 {
		return nil, errors.New(contentlang.Pick("当前问题只允许单选", "Câu hỏi này chỉ cho phép chọn một"))
	}

	seen := make(map[int]bool, len(parts))
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New(contentlang.Pick("编号不能为空", "Số thứ tự không được để trống"))
		}

		var idx int
		if _, err := fmt.Sscanf(part, "%d", &idx); err != nil {
			return nil, errors.New(contentlang.Pick(
				fmt.Sprintf("无法识别编号 %q", part),
				fmt.Sprintf("Không nhận dạng được số thứ tự %q", part)))
		}
		if idx <= 0 || idx > len(options) {
			return nil, errors.New(contentlang.Pick(
				fmt.Sprintf("编号 %d 超出范围", idx),
				fmt.Sprintf("Số thứ tự %d ngoài phạm vi", idx)))
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		labels = append(labels, options[idx-1].Label)
	}
	if len(labels) == 0 {
		return nil, errors.New(contentlang.Pick("至少选择一个选项", "Hãy chọn ít nhất một lựa chọn"))
	}
	return labels, nil
}
