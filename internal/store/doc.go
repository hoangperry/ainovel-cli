// Package store cung cấp lưu trữ bền vững dựa trên hệ thống tập tin.
//
// Kiến trúc: 1 nền IO + nhiều sub-store + 1 composition root.
// Mỗi sub-store giữ một instance IO độc lập và một sync.RWMutex độc lập.
// Việc đọc/ghi của các domain chính (Progress, Outline, Drafts, Summaries...) không chặn lẫn nhau;
// WorldStore gộp nhiều domain nhỏ tần suất thấp dùng chung một khóa.
//
// Composition root Store giữ tham chiếu tới mọi sub-store và chịu trách nhiệm thao tác nguyên tử xuyên domain
// （ExpandArc、AppendVolume、ClearHandledSteer）。
//
// Phân chia sub-store:
//   - ProgressStore: trạng thái tiến độ chính (meta/progress.json)
//   - OutlineStore: tiền đề, dàn ý (phẳng/phân tầng), la bàn
//   - DraftStore: phác thảo chương, bản nháp, bản cuối
//   - SummaryStore: tóm tắt chương/cung/quyển
//   - RunMetaStore: metadata vận hành (model, lịch sử can thiệp)
//   - SignalStore: file tín hiệu dùng một lần (khôi phục PendingCommit)
//   - CheckpointStore: checkpoint cấp step (meta/checkpoints.jsonl)
//   - RuntimeStore: hàng đợi sự kiện runtime (meta/runtime/*.jsonl)
//   - CharacterStore: hồ sơ nhân vật, snapshot trạng thái
//   - WorldStore: dòng thời gian, phục bút, quan hệ, thay đổi trạng thái, luật thế giới, luật văn phong, rà soát
package store
