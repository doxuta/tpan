package store

import (
	"encoding/json"

	"github.com/doxuta/tpan/internal/engine"
)

// marshalEvent và unmarshalEvent là chỗ duy nhất biết cột data là JSON của
// engine.Event. Event có tag json sẵn (dùng cho SSE), nên lưu đúng dạng đó:
// Event.Check (checks.Result kèm Evidence) khứ hồi đầy đủ mà không cần bảng riêng.
func marshalEvent(ev engine.Event) (string, error) {
	b, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalEvent(data string) (engine.Event, error) {
	var ev engine.Event
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return engine.Event{}, err
	}
	return ev, nil
}
