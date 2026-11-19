package core

// Hai Step riêng của core cho Plan một chiều (nâng MariaDB, mục 6.11). Plan một
// chiều có một điểm không quay lại: gói đã nâng thì dữ liệu đã nâng. Mọi thứ
// nằm sau điểm đó, nếu engine hoàn tác, sẽ để máy nói sai về chính nó (đang chạy
// dòng mới mà kho và settings nói dòng cũ). Vì vậy kho mới không hoàn tác, và
// settings được ghi bằng một bước ngay sau khi nâng gói, không đợi job thành công.

import (
	"context"
	"fmt"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// keepOnUndo bọc một Step: Apply, Check như cũ, Undo không làm gì, và Info nói
// rõ vì sao.
type keepOnUndo struct {
	engine.Step
	why i18n.Text
}

func (k *keepOnUndo) Info() engine.StepInfo {
	info := k.Step.Info()
	info.NoUndo = k.why
	return info
}

func (k *keepOnUndo) Undo(context.Context, system.System) error { return nil }

// settingsStep ghi các cặp khoá, giá trị vào settings như một bước của Plan.
// Check: mọi khoá đã có đúng giá trị. Không hoàn tác (xem đầu file).
type settingsStep struct {
	s     Settings
	kv    [][2]string
	title i18n.Text
	why   i18n.Text
}

func (st *settingsStep) Info() engine.StepInfo {
	id := "settings"
	for _, kv := range st.kv {
		id += ":" + kv[0] + "=" + kv[1]
	}
	return engine.StepInfo{ID: id, Title: st.title, Why: st.why, NoUndo: i18n.T(
		"Không hoàn tác: bước này chỉ chạy sau khi gói đã nâng, mà gói đã nâng thì máy đang chạy dòng mới; trả settings về dòng cũ là để tpan nói sai về máy.",
		"Not undone: this step only runs after the packages are upgraded, and then the machine runs the new series; putting the old series back in settings would make tpan describe the machine wrongly.")}
}

func (st *settingsStep) Check(ctx context.Context, _ system.System) (bool, error) {
	for _, kv := range st.kv {
		v, _, err := st.s.GetSetting(ctx, kv[0])
		if err != nil {
			return false, fmt.Errorf("đọc setting %s: %w", kv[0], err)
		}
		if v != kv[1] {
			return false, nil
		}
	}
	return true, nil
}

func (st *settingsStep) Apply(ctx context.Context, _ system.System) error {
	for _, kv := range st.kv {
		if err := st.s.SetSetting(ctx, kv[0], kv[1]); err != nil {
			return fmt.Errorf("ghi setting %s: %w", kv[0], err)
		}
	}
	return nil
}

func (st *settingsStep) Undo(context.Context, system.System) error { return nil }
