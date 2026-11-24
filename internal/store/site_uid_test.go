package store

import (
	"context"
	"sync"
	"testing"
)

// F1 (M28 đợt 2): số đã cấp không bao giờ được trả lại, kể cả khi người gọi
// đưa floor thấp hơn; floor cao hơn thì nhảy lên floor.
func TestReserveSiteUID(t *testing.T) {
	ctx := context.Background()
	s, path := openTemp(t)

	steps := []struct{ floor, want int }{
		{65536, 65536}, // lần đầu: đúng floor
		{65536, 65537}, // floor cũ: số kế tiếp, không dùng lại 65536
		{100, 65538},   // floor thấp hơn: vẫn tăng
		{70000, 70000}, // máy đã có UID cao hơn: nhảy lên
		{65536, 70001},
	}
	for _, st := range steps {
		got, err := s.ReserveSiteUID(ctx, st.floor)
		if err != nil {
			t.Fatal(err)
		}
		if got != st.want {
			t.Fatalf("ReserveSiteUID(%d) = %d, muốn %d", st.floor, got, st.want)
		}
	}
	if v, ok, _ := s.GetSetting(ctx, SiteUIDKey); !ok || v != "70001" {
		t.Errorf("settings[%s] = %q, %v; muốn 70001", SiteUIDKey, v, ok)
	}

	// Mở lại tpan.db: con số còn đó.
	_ = s.Close()
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	if got, err := s2.ReserveSiteUID(ctx, 65536); err != nil || got != 70002 {
		t.Errorf("sau khi mở lại = %d, %v; muốn 70002", got, err)
	}
}

// Hai tiến trình (panel và CLI) cùng cấp: mỗi lần một số khác nhau.
func TestReserveSiteUIDDongThoi(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	const n = 20
	var (
		mu   sync.Mutex
		seen = map[int]bool{}
		wg   sync.WaitGroup
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.ReserveSiteUID(ctx, 65536)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if seen[got] {
				t.Errorf("số %d bị cấp hai lần", got)
			}
			seen[got] = true
		}()
	}
	wg.Wait()
	if len(seen) != n {
		t.Errorf("cấp %d số khác nhau, muốn %d", len(seen), n)
	}
}
