package ai

// Lõi AI trong panel (hợp đồng ở panel_types.go, đặc tả ở mục 8.4 của
// docs/KE-HOACH.md).
//
// Ba nguồn dữ liệu, ba mức tin:
//   - AskInput do trình duyệt gửi: chỉ dùng làm nhãn (mục đang mở, đường vừa
//     đi) và để biết đang chỉ vào cái gì (Pointer.Kind, Ref). Bằng chứng thì
//     máy chủ tự đọc từ Store theo Ref, không tin bằng chứng trình duyệt gửi.
//   - Store: bản ghi của chính TPAN. Riêng output lệnh, thân trang, dòng log
//     là chữ do người lạ viết nên được bọc Untrusted.
//   - Sự kiện của bộ não: chữ AI viết, ở dạng bí danh; chỉ đổi về tên thật
//     ngay trước khi đưa ra trình duyệt.
//
// Bảng bí danh của một phiên có hai người ghi: Panel (lúc tạo phiên và đầu mỗi
// lượt, trước khi gửi Job) và tiến trình `tpan mcp --session` (sau mỗi lần AI
// gọi công cụ). Hai bên không ghi cùng lúc: trong lúc lượt chạy Panel chỉ đọc,
// và mọi lần Panel ghi đều đọc lại phiên từ DB ngay trước đó.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/ai/playbooks"
	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	// maxChipRunes: mỗi khối ngữ cảnh tối đa chừng này rune, để một output
	// lệnh dài không ăn hết hạn mức của lượt hỏi.
	maxChipRunes = 8000
	// maxUntrustedRunes: phần chữ của người lạ được cắt TRƯỚC khi bọc, để thẻ
	// đóng </untrusted> không bao giờ bị cắt mất (mất thẻ đóng thì phần sau
	// của prompt lẫn vào vùng "dữ liệu").
	maxUntrustedRunes = 6000
	// viewerBuffer: số sự kiện xếp hàng cho một người xem. Đầy thì người xem
	// đó bị bỏ (kênh đóng), để một trình duyệt chậm không làm kẹt lượt.
	viewerBuffer = 64
)

// chipLabels là nhãn của từng khối trên ngăn AI. Cố định, không chứa tên thật:
// nhãn được lưu cùng lượt (ai_turns.context), mà bảng đó chỉ giữ bản đã che.
var chipLabels = map[string]i18n.Text{
	ChipView:     i18n.T("Đang ở", "Viewing"),
	ChipPointer:  i18n.T("Đang chỉ vào", "Pointing at"),
	ChipTrail:    i18n.T("Vừa đi qua", "Recent path"),
	ChipEvidence: i18n.T("Bằng chứng", "Evidence"),
	ChipMachine:  i18n.T("Bối cảnh máy", "Machine"),
}

// panelState giữ lượt đang chạy của từng phiên và người đang xem chúng.
type panelState struct {
	// askMu xếp hàng phần kiểm và ghi của Ask: hai câu hỏi cùng lúc vào một
	// phiên thì câu sau phải thấy lượt running của câu trước.
	askMu sync.Mutex
	// mu giữ runs. Thứ tự khoá: mu trước, turnRun.mu sau.
	mu   sync.Mutex
	runs map[string]*turnRun // id phiên → lượt đang chạy (mỗi phiên một lượt)
}

// turnRun là một lượt đang chạy trong tiến trình này.
type turnRun struct {
	session string
	seq     int

	mu      sync.Mutex
	red     *Redactor // chỉ để Unalias; thay mới sau mỗi tool_result
	raw     []Event   // đúng như bộ não gửi (bí danh), để lưu
	shown   []Event   // bản hiển thị (tên thật)
	sh      shower
	viewers map[*viewer]struct{}
}

type viewer struct {
	ch     chan Event
	closed bool
}

func (p *Panel) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func refuse(code, vi, en string) error {
	return &core.RefusedError{Code: code, Message: i18n.T(vi, en)}
}

func normLang(l string) string {
	if l == "en" {
		return "en"
	}
	return "vi"
}

// ---------------------------------------------------------------- Preview, Ask

// Preview: xem hợp đồng ở panel_types.go. Câu hỏi rỗng được nhận (giao diện
// gọi ngay khi mở ngăn để hiện nhãn ngữ cảnh); khi đó prompt không có khối
// <question>.
func (p *Panel) Preview(ctx context.Context, in AskInput) (AskPreview, error) {
	if err := checkLen(in.Question); err != nil {
		return AskPreview{}, err
	}
	var (
		sess store.AISession
		prev []store.AITurn
		err  error
	)
	if in.SessionID != "" {
		if sess, prev, err = p.loadSession(ctx, in.SessionID); err != nil {
			return AskPreview{}, err
		}
	}
	red, err := p.redactor(ctx, sess.Aliases)
	if err != nil {
		return AskPreview{}, err
	}
	b, err := p.build(ctx, in, red, prev)
	if err != nil {
		return AskPreview{}, err
	}
	return AskPreview{SessionID: in.SessionID, Question: b.question, Chips: b.chips, Playbook: b.playbook, Prompt: b.prompt}, nil
}

func checkLen(q string) error {
	if utf8.RuneCountInString(strings.TrimSpace(q)) > MaxQuestionLen {
		return refuse("question_too_long",
			fmt.Sprintf("Câu hỏi dài quá %d ký tự.", MaxQuestionLen),
			fmt.Sprintf("The question is longer than %d characters.", MaxQuestionLen))
	}
	return nil
}

// Ask: xem hợp đồng ở panel_types.go.
func (p *Panel) Ask(ctx context.Context, in AskInput) (AskStarted, error) {
	if p.Brain == nil {
		return AskStarted{}, refuse("ai_off", "AI đang tắt (setting ai.brain = off).", "AI is turned off (setting ai.brain = off).")
	}
	if strings.TrimSpace(in.Question) == "" {
		return AskStarted{}, refuse("question_empty", "Chưa có câu hỏi.", "The question is empty.")
	}
	if err := checkLen(in.Question); err != nil {
		return AskStarted{}, err
	}
	r, job, err := p.start(ctx, in)
	if err != nil {
		return AskStarted{}, err
	}
	// Lượt sống lâu hơn request (request trả về ngay), nên không dùng ctx của
	// request; TurnTimeout là chốt chặn lượt chạy mãi.
	bctx, cancel := context.WithTimeout(context.Background(), TurnTimeout)
	ch, err := p.Brain.Run(bctx, job)
	if err != nil {
		cancel()
		p.finish(r, Event{Kind: EventError, Text: err.Error()})
		switch {
		case errors.Is(err, ErrBrainOffline):
			return AskStarted{}, refuse("brain_offline",
				"Chưa có bộ não nào nối vào panel. Chạy tpan brain <máy> trên máy của bạn.",
				"No brain is connected to the panel. Run tpan brain <host> on your computer.")
		case errors.Is(err, ErrBrainBusy):
			return AskStarted{}, refuse("brain_busy",
				"Bộ não đang trả lời một câu hỏi khác. Đợi xong rồi hỏi lại.",
				"The brain is answering another question. Ask again when it is done.")
		}
		return AskStarted{}, fmt.Errorf("gửi câu hỏi %s cho bộ não: %w", job.ID, err)
	}
	go p.receive(r, ch, cancel)
	return AskStarted{SessionID: r.session, Seq: r.seq}, nil
}

// start làm phần kiểm và ghi của Ask (dưới askMu): mở phiên nếu mới, lưu bảng
// bí danh, ghi lượt running, đăng ký lượt để Subscribe thấy ngay.
func (p *Panel) start(ctx context.Context, in AskInput) (*turnRun, Job, error) {
	p.st.askMu.Lock()
	defer p.st.askMu.Unlock()

	var (
		sess store.AISession
		prev []store.AITurn
		err  error
	)
	isNew := in.SessionID == ""
	if !isNew {
		if sess, prev, err = p.loadSession(ctx, in.SessionID); err != nil {
			return nil, Job{}, err
		}
		running := p.runOf(sess.ID) != nil
		for _, t := range prev {
			// Lượt running trong DB mà không có ở bộ nhớ: tiến trình trước chết
			// giữa lượt và Recover chưa chạy. Vẫn từ chối, đợi Recover dọn.
			running = running || t.Status == store.TurnRunning
		}
		if running {
			return nil, Job{}, refuse("ai_turn_running",
				"Phiên này đang có một câu hỏi chưa trả lời xong.",
				"This session is still answering a question.")
		}
	}
	now := p.now()
	n, err := p.Store.CountAITurnsSince(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return nil, Job{}, err
	}
	if n >= MaxQuestionsPerDay {
		return nil, Job{}, refuse("ai_daily_limit",
			fmt.Sprintf("Đã hỏi %d câu trong 24 giờ qua (tối đa %d). Đợi rồi hỏi lại.", n, MaxQuestionsPerDay),
			fmt.Sprintf("%d questions were asked in the last 24 hours (the limit is %d). Wait and ask again.", n, MaxQuestionsPerDay))
	}

	red, err := p.redactor(ctx, sess.Aliases)
	if err != nil {
		return nil, Job{}, err
	}
	b, err := p.build(ctx, in, red, prev)
	if err != nil {
		return nil, Job{}, err
	}
	sent, err := json.Marshal(b.sent())
	if err != nil {
		return nil, Job{}, fmt.Errorf("mã hoá khối ngữ cảnh: %w", err)
	}

	// Lưu bảng bí danh SAU khi dựng: câu hỏi và bằng chứng có thể vừa sinh bí
	// danh mới, mà tpan mcp (và lúc hiển thị) cần đổi được chúng về tên thật.
	if isNew {
		id, err := newSessionID()
		if err != nil {
			return nil, Job{}, err
		}
		uuid, err := newUUID()
		if err != nil {
			return nil, Job{}, err
		}
		sess = store.AISession{
			ID: id, Entry: store.EntryPanel, Question: b.question, Context: sent,
			Aliases: red.Aliases(), Untrusted: b.untrusted, BrainSession: uuid, Created: now, Last: now,
		}
		if err := p.Store.CreateAISession(ctx, sess); err != nil {
			return nil, Job{}, err
		}
	} else {
		// sess vừa đọc từ DB ở loadSession (cùng askMu, không lượt nào chạy)
		// nên không đè mất thứ tpan mcp đã ghi.
		sess.Aliases = red.Aliases()
		// Prompt mang nội dung của người lạ thì Plan AI đề xuất sau đó phải
		// mang cảnh báo (mục 8.1, ý 5): tpan mcp đọc cờ này của phiên.
		sess.Untrusted = sess.Untrusted || b.untrusted
		sess.Last = now
		if err := p.Store.SaveAISessionState(ctx, sess); err != nil {
			return nil, Job{}, err
		}
	}

	seq := 1
	resume := false
	for _, t := range prev {
		seq = max(seq, t.Seq+1)
		resume = resume || reachedBrain(t)
	}
	lang := normLang(in.Lang)
	turn := store.AITurn{
		SessionID: sess.ID, Seq: seq, Lang: lang, Question: b.question, Context: sent,
		Playbook: b.playbook, Prompt: b.prompt, Status: store.TurnRunning, Started: now,
	}
	if err := p.Store.CreateAITurn(ctx, turn); err != nil {
		return nil, Job{}, err
	}
	r := &turnRun{session: sess.ID, seq: seq, red: NewRedactor(LevelAll, sess.Aliases), viewers: map[*viewer]struct{}{}}
	p.st.mu.Lock()
	if p.st.runs == nil {
		p.st.runs = map[string]*turnRun{}
	}
	p.st.runs[sess.ID] = r
	p.st.mu.Unlock()

	return r, Job{
		ID: fmt.Sprintf("%s-%d", sess.ID, seq), SessionID: sess.ID, BrainSession: sess.BrainSession,
		Resume: resume, Prompt: b.prompt, Lang: lang,
	}, nil
}

// loadSession đọc một phiên panel và các lượt của nó.
func (p *Panel) loadSession(ctx context.Context, id string) (store.AISession, []store.AITurn, error) {
	sess, err := p.Store.AISession(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.AISession{}, nil, refuse("ai_session_not_found",
			"Không có phiên AI này.", "There is no such AI session.")
	}
	if err != nil {
		return store.AISession{}, nil, err
	}
	// Phiên mcp là của một agent nối trực tiếp: hỏi tiếp vào đó thì dùng chung
	// bảng bí danh và Plan với agent kia.
	if sess.Entry != store.EntryPanel {
		return store.AISession{}, nil, refuse("ai_session_not_panel",
			"Phiên này của một agent nối trực tiếp, không hỏi tiếp từ panel được.",
			"This session belongs to a directly connected agent and cannot be continued from the panel.")
	}
	turns, err := p.Store.AITurns(ctx, id)
	if err != nil {
		return store.AISession{}, nil, err
	}
	return sess, turns, nil
}

// redactor dựng bộ che cho một lượt giống hệt tools.OpenSession: cùng mức che,
// cùng bảng bí danh đã lưu, Learn mọi site và hostname. Nhờ vậy bí danh trong
// prompt trùng bí danh mà tiến trình tpan mcp của lượt đó dùng.
func (p *Panel) redactor(ctx context.Context, aliases map[string]string) (*Redactor, error) {
	cfg, err := core.AgentConfig(ctx, p.Store)
	if err != nil {
		return nil, fmt.Errorf("dựng bộ che: %w", err)
	}
	red := NewRedactor(Level(cfg.Redact), aliases)
	sites, err := p.Store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("dựng bộ che: đọc danh sách site: %w", err)
	}
	domains := make([]validate.Domain, len(sites))
	for i, s := range sites {
		domains[i] = s.Domain
	}
	red.Learn(domains, p.Hostname)
	return red, nil
}

// reachedBrain: lượt đã tới được claude (có sự kiện init), tức claude đã có
// phiên để --resume và đã đọc bản hướng dẫn gửi kèm. Lượt hỏng trước đó (bộ
// não chưa nối, ssh lỗi) thì chưa. Lượt đang chạy coi như đã tới.
func reachedBrain(t store.AITurn) bool {
	if t.Status == store.TurnRunning {
		return true
	}
	var evs []Event
	if json.Unmarshal(t.Events, &evs) != nil {
		return false
	}
	return slices.ContainsFunc(evs, func(e Event) bool { return e.Kind == EventInit })
}

func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sinh id phiên AI: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// newUUID sinh UUID phiên bản 4 (RFC 9562) cho --session-id của claude.
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sinh UUID phiên Claude: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40 // phiên bản 4
	b[8] = b[8]&0x3f | 0x80 // biến thể RFC
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// ------------------------------------------------------------------- dựng prompt

type built struct {
	question  string
	chips     []PreviewChip
	playbook  string
	prompt    string
	untrusted bool // một khối được gửi có nội dung Untrusted
}

func (b built) sent() []Chip {
	out := []Chip{}
	for _, c := range b.chips {
		if !c.Omitted {
			out = append(out, c.Chip)
		}
	}
	return out
}

// build dựng câu hỏi, các khối và prompt. Preview và Ask cùng gọi hàm này với
// cùng đầu vào nên ra cùng một prompt: bộ che gán bí danh mới theo thứ tự gặp,
// mà thứ tự che ở đây cố định (câu hỏi, rồi các khối theo ChipKeys; khối bị bỏ
// vẫn được che để Preview hiện được).
func (p *Panel) build(ctx context.Context, in AskInput, red *Redactor, prev []store.AITurn) (built, error) {
	lang := normLang(in.Lang)
	var b built
	b.question = red.String(strings.TrimSpace(in.Question))
	raw, err := p.chipContents(ctx, in, lang)
	if err != nil {
		return built{}, err
	}
	omit := map[string]bool{}
	for _, k := range in.Omit {
		omit[k] = true
	}
	b.chips = []PreviewChip{}
	for _, key := range ChipKeys {
		content, ok := raw[key]
		if !ok {
			continue
		}
		c := PreviewChip{Chip: Chip{Key: key, Label: chipLabels[key], Content: clip(red.String(content))}, Omitted: omit[key]}
		b.chips = append(b.chips, c)
		if !c.Omitted && strings.Contains(c.Content, "<untrusted ") {
			b.untrusted = true
		}
	}

	var pb strings.Builder
	fmt.Fprintf(&pb, "<tpan-context lang=%q>\n", lang)
	for _, c := range b.chips {
		if !c.Omitted {
			pb.WriteString("[" + c.Key + "] " + c.Content + "\n")
		}
	}
	pb.WriteString("</tpan-context>\n")
	if name, text, ok := playbooks.For(in.View.Path); ok && !playbookSent(prev, name) {
		b.playbook = name
		// Bản hướng dẫn KHÔNG qua bộ che: nó là chữ tĩnh nhúng trong tpan,
		// không có tên thật hay bí mật nào, mà bộ che lại làm méo nó (bản
		// co-so-du-lieu có "change its password: answer in words only", luật
		// password: ... sẽ xoá chữ "answer").
		fmt.Fprintf(&pb, "<tpan-playbook name=%q>\n%s\n</tpan-playbook>\n", name, strings.TrimSpace(text))
	}
	if b.question != "" {
		pb.WriteString("<question>\n" + b.question + "\n</question>\n")
	}
	b.prompt = pb.String()
	return b, nil
}

func playbookSent(prev []store.AITurn, name string) bool {
	return slices.ContainsFunc(prev, func(t store.AITurn) bool { return t.Playbook == name && reachedBrain(t) })
}

// chipContents trả nội dung (chưa che) của các khối dựng được, theo khoá.
// Không có Pointer thì không có khối pointer và evidence.
func (p *Panel) chipContents(ctx context.Context, in AskInput, lang string) (map[string]string, error) {
	out := map[string]string{}
	if v := viewLine(in.View); v != "" {
		out[ChipView] = v
	}
	if ptr := in.Pointer; ptr != nil {
		s := "kind: " + oneLine(ptr.Kind) + "\nref: " + oneLine(ptr.Ref)
		if ptr.Label != "" {
			s += "\nlabel: " + oneLine(ptr.Label)
		}
		out[ChipPointer] = s
		ev, err := p.evidence(ctx, *ptr, lang)
		if err != nil {
			return nil, fmt.Errorf("đọc bằng chứng cho %s %s: %w", ptr.Kind, ptr.Ref, err)
		}
		out[ChipEvidence] = ev
	}
	trail := in.Trail
	if len(trail) > MaxTrail {
		trail = trail[len(trail)-MaxTrail:]
	}
	var lines []string
	for _, v := range trail {
		if l := viewLine(v); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 0 {
		out[ChipTrail] = strings.Join(lines, "\n")
	}
	m, err := p.machine(ctx, lang)
	if err != nil {
		return nil, err
	}
	out[ChipMachine] = m
	return out, nil
}

// oneLine gộp xuống dòng: nhãn do trình duyệt gửi không được tự mở một dòng
// "[evidence] ..." giả trong prompt.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func viewLine(v View) string {
	label, path := oneLine(v.Label), oneLine(v.Path)
	switch {
	case label != "" && path != "":
		return label + " (" + path + ")"
	case path != "":
		return path
	}
	return label
}

// clip cắt khối quá dài. Nếu chỗ cắt nằm trong một vùng untrusted thì đóng
// vùng đó lại (Untrusted đã vô hiệu mọi thẻ nằm sẵn trong chữ, nên đếm thẻ là
// đếm đúng).
func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxChipRunes {
		return s
	}
	const tail = "\n[... cut]"
	s = cutRunes(s, maxChipRunes-40) + tail
	if strings.Count(s, "<untrusted ") > strings.Count(s, "</untrusted>") {
		s += "\n</untrusted>"
	}
	return s
}

func cutRunes(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}

func untrusted(source, text string) string {
	if utf8.RuneCountInString(text) > maxUntrustedRunes {
		text = cutRunes(text, maxUntrustedRunes) + "\n[... cut]"
	}
	return Untrusted(source, text)
}

func tx(t i18n.Text, lang string) string {
	if lang == "en" && t.En != "" || t.Vi == "" {
		return t.En
	}
	return t.Vi
}

func fmtT(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// evidence đọc bằng chứng của thứ đang chỉ vào, từ Store. Ref do trình duyệt
// gửi: chỉ dùng làm khoá tra cứu (domain qua validate, id sự cố qua
// strconv), không ghép vào lệnh hay đường dẫn nào.
func (p *Panel) evidence(ctx context.Context, ptr Pointer, lang string) (string, error) {
	// "not found" đứng trước: Ref đứng trước dấu ":" thì bộ che có thể coi
	// Ref là tên khoá (passwd: ...) và che mất chữ sau nó.
	notFound := "not found: " + oneLine(ptr.Kind) + " " + oneLine(ptr.Ref)
	var b strings.Builder
	switch ptr.Kind {
	case PointCheck:
		r, ok, err := p.Store.CheckResult(ctx, ptr.Ref)
		if err != nil || !ok {
			return notFound, err
		}
		writeCheck(&b, r, lang)

	case PointIncident:
		id, err := strconv.ParseInt(ptr.Ref, 10, 64)
		if err != nil {
			return notFound, nil
		}
		inc, err := p.Store.Incident(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return notFound, nil
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "incident %d\ncheck: %s\ntarget: %s\nstage: %s\ntitle: %s\nopened: %s\n",
			inc.ID, inc.CheckID, targetName(inc.Target), inc.Stage, tx(inc.Title, lang), fmtT(inc.Opened))
		if inc.Closed.IsZero() {
			b.WriteString("state: open\n")
		} else {
			fmt.Fprintf(&b, "state: closed %s (%s)\n", fmtT(inc.Closed), inc.CloseReason)
		}
		if len(inc.Jobs) > 0 {
			b.WriteString("fix jobs: " + strings.Join(inc.Jobs, ", ") + "\n")
		}
		r, ok, err := p.Store.CheckResult(ctx, inc.CheckID)
		if err != nil {
			return "", err
		}
		if !ok {
			b.WriteString("current result of the check: none (no longer checked)\n")
		} else {
			b.WriteString("current result of the check:\n")
			writeCheck(&b, r, lang)
		}

	case PointJob:
		j, err := p.Store.Job(ctx, ptr.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return notFound, nil
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "job %s\nkind: %s\ntarget: %s\ntitle: %s\nstatus: %s\nstarted: %s\n",
			j.ID, j.Kind, j.Target, tx(j.Title, lang), j.Status, fmtT(j.Started))
		if !j.Finished.IsZero() {
			b.WriteString("finished: " + fmtT(j.Finished) + "\n")
		}
		evs, err := p.Store.Events(ctx, j.ID, 0)
		if err != nil {
			return "", err
		}
		// Lời lỗi của bước thường chứa stderr của lệnh: chữ do chương trình
		// khác viết, có thể mang nội dung file của site, nên bọc Untrusted.
		var errs []string
		if j.Error != "" {
			errs = append(errs, "job error: "+j.Error)
		}
		for _, ev := range evs {
			if ev.Kind == engine.EventStepFail {
				errs = append(errs, "step "+ev.StepID+" failed: "+ev.Message)
			}
		}
		if len(errs) == 0 {
			b.WriteString("failed steps: none\n")
		} else {
			b.WriteString(untrusted("job errors", strings.Join(errs, "\n")))
		}

	case PointJobStep:
		jobID, stepID, ok := strings.Cut(ptr.Ref, "/")
		if !ok || jobID == "" || stepID == "" {
			return notFound, nil
		}
		j, err := p.Store.Job(ctx, jobID)
		if errors.Is(err, store.ErrNotFound) {
			return notFound, nil
		}
		if err != nil {
			return "", err
		}
		evs, err := p.Store.Events(ctx, j.ID, 0)
		if err != nil {
			return "", err
		}
		var lines []string
		for _, ev := range evs {
			if ev.StepID == stepID {
				lines = append(lines, string(ev.Kind)+": "+ev.Message)
			}
		}
		if len(lines) == 0 {
			return notFound, nil
		}
		fmt.Fprintf(&b, "job %s (%s, %s), step %s\n", j.ID, j.Kind, j.Status, stepID)
		b.WriteString(untrusted("job step events", strings.Join(lines, "\n")))

	case PointLogLine:
		if strings.TrimSpace(ptr.Text) == "" {
			return notFound, nil
		}
		src := "log"
		if d, kind, ok := strings.Cut(ptr.Ref, "/"); ok && (kind == "access" || kind == "error" || kind == "php") {
			if dom, err := validate.ParseDomain(d); err == nil {
				src = "log " + dom.String() + "/" + kind
			}
		}
		b.WriteString(untrusted(src, ptr.Text))

	case PointPlan:
		pl, err := p.Store.Plan(ctx, ptr.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return notFound, nil
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "plan %s\nkind: %s\ntarget: %s\ntitle: %s\nstatus: %s\norigin: %s\n",
			pl.ID, pl.Kind, pl.Target, tx(pl.Title, lang), pl.Status, pl.Origin)
		if pl.Untrusted {
			b.WriteString("warning: proposed after reading untrusted content\n")
		}
		b.WriteString("steps:\n")
		for i, s := range pl.Preview.Steps {
			state := "will run"
			if s.Done {
				state = "already done, skipped"
			}
			fmt.Fprintf(&b, "%d. %s: %s (%s", i+1, s.Info.ID, tx(s.Info.Title, lang), state)
			if s.Info.Danger {
				b.WriteString(", danger")
			}
			b.WriteString(")\n")
			for _, c := range s.Info.Commands {
				b.WriteString("   $ " + c + "\n")
			}
			for _, f := range s.Info.Files {
				act := "write"
				if f.Delete {
					act = "delete"
				}
				b.WriteString("   " + act + " " + f.Path + "\n")
			}
		}

	case PointSite:
		d, err := validate.ParseDomain(ptr.Ref)
		if err != nil {
			return notFound, nil
		}
		s, ok, err := p.Store.Site(ctx, d)
		if err != nil || !ok {
			return notFound, err
		}
		fmt.Fprintf(&b, "site %s\nstatus: %s\nphp: %s\nssl: %t\ncreated: %s\n",
			s.Domain, s.Status, s.PHPVersion, s.SSL, fmtT(s.Created))
		rs, err := p.Store.CheckResults(ctx)
		if err != nil {
			return "", err
		}
		b.WriteString("stored check results:\n")
		n := 0
		for _, r := range rs {
			if r.Target != d.String() {
				continue
			}
			n++
			fmt.Fprintf(&b, "- [%s] %s (%s): %s: %s\n", r.Status, r.ID, r.Stage, tx(r.Title, lang), tx(r.Summary, lang))
		}
		if n == 0 {
			b.WriteString("none yet\n")
		}

	default:
		return notFound, nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func targetName(t string) string {
	if t == "" {
		return "machine"
	}
	return t
}

// writeCheck ghi một kết quả phép kiểm đã lưu. Lệnh và output nằm trong
// Untrusted: output có thể chứa thân trang hay dòng log do người lạ viết.
func writeCheck(b *strings.Builder, r store.StoredResult, lang string) {
	fmt.Fprintf(b, "check %s\ntarget: %s\nstage: %s\nstatus: %s\ntitle: %s\nsummary: %s\nchecked: %s\nstatus since: %s\n",
		r.ID, targetName(r.Target), r.Stage, r.Status, tx(r.Title, lang), tx(r.Summary, lang), fmtT(r.Checked), fmtT(r.Changed))
	if c := tx(r.Cause, lang); c != "" {
		b.WriteString("possible cause: " + c + "\n")
	}
	if len(r.ManualFix) > 0 {
		b.WriteString("manual fix:\n")
		for _, c := range r.ManualFix {
			b.WriteString("   $ " + c + "\n")
		}
	}
	if r.AutoFix != "" {
		b.WriteString("auto fix available: " + r.AutoFix + "\n")
	}
	if len(r.Evidence) > 0 {
		var e strings.Builder
		for i, ev := range r.Evidence {
			if i > 0 {
				e.WriteString("\n")
			}
			fmt.Fprintf(&e, "$ %s\nexit code: %d\n%s", ev.Command, ev.ExitCode, ev.Output)
		}
		b.WriteString(untrusted("check evidence", e.String()) + "\n")
	}
}

// machine là bối cảnh máy (mục 8.4): đọc từ settings như core.
func (p *Panel) machine(ctx context.Context, lang string) (string, error) {
	get := func(k string) (string, error) {
		v, ok, err := p.Store.GetSetting(ctx, k)
		if err != nil {
			return "", fmt.Errorf("đọc setting %s: %w", k, err)
		}
		if !ok || v == "" {
			return "not set", nil
		}
		return v, nil
	}
	web, err := get(core.SettingWeb)
	if err != nil {
		return "", err
	}
	php, err := get(core.SettingPHPVersion)
	if err != nil {
		return "", err
	}
	sites, err := p.Store.Sites(ctx)
	if err != nil {
		return "", fmt.Errorf("đọc danh sách site: %w", err)
	}
	active := 0
	for _, s := range sites {
		if s.Status == site.Active {
			active++
		}
	}
	return fmt.Sprintf("web server: %s\ndefault php: %s\nactive sites: %d\ntpan version: %s\nui language: %s",
		web, php, active, buildinfo.Version, lang), nil
}

// ------------------------------------------------------------- nhận sự kiện

// shower đổi sự kiện của bộ não thành sự kiện hiển thị. Chữ được nối lại rồi
// mới đổi bí danh: một bí danh có thể bị cắt giữa hai phần chữ.
type shower struct{ acc strings.Builder }

func (s *shower) show(ev Event, red *Redactor) Event {
	switch ev.Kind {
	case EventText:
		s.acc.WriteString(ev.Text)
		ev.Text = red.Unalias(s.acc.String())
	case EventDone:
		// Ưu tiên chữ đã nối: trường result của Claude Code chỉ giữ tin nhắn
		// cuối, mất các đoạn AI viết trước khi gọi công cụ (thấy ở lần gọi
		// thật #4). result chỉ dùng khi không có phần chữ nào.
		if s.acc.Len() > 0 {
			ev.Text = s.acc.String()
		}
		ev.Text = red.Unalias(ev.Text)
	case EventToolCall:
		ev.Input = unaliasJSON(red, ev.Input)
		ev.Text = red.Unalias(ev.Text)
	default:
		ev.Text = red.Unalias(ev.Text)
	}
	return ev
}

// showEvents dựng lại bản hiển thị từ sự kiện đã lưu (lượt đã xong).
func showEvents(raw []Event, red *Redactor) (shown []Event, acc string) {
	var sh shower
	shown = make([]Event, 0, len(raw))
	for _, ev := range raw {
		shown = append(shown, sh.show(ev, red))
	}
	return shown, sh.acc.String()
}

// unaliasJSON đổi bí danh trên từng giá trị chuỗi (không trên cả khối JSON,
// để không chạm vào tên khoá), như tools.unaliasArgs.
func unaliasJSON(red *Redactor, raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		// Không phải JSON: hiện thành một chuỗi JSON để Event vẫn mã hoá được.
		out, _ := json.Marshal(red.Unalias(string(raw)))
		return out
	}
	out, err := json.Marshal(unaliasValue(red, v))
	if err != nil {
		return raw
	}
	return out
}

func unaliasValue(red *Redactor, v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = unaliasValue(red, x)
		}
	case []any:
		for i := range t {
			t[i] = unaliasValue(red, t[i])
		}
	case string:
		return red.Unalias(t)
	}
	return v
}

// receive nhận sự kiện của một lượt tới khi kết thúc.
func (p *Panel) receive(r *turnRun, ch <-chan Event, cancel context.CancelFunc) {
	defer cancel()
	for ev := range ch {
		if ev.Time.IsZero() {
			ev.Time = p.now()
		}
		if ev.Final() {
			p.finish(r, ev)
			return
		}
		if ev.Kind == EventToolResult {
			// tpan mcp có thể vừa thêm bí danh (site mới, IP trong log).
			if sess, err := p.Store.AISession(context.Background(), r.session); err == nil {
				red := NewRedactor(LevelAll, sess.Aliases)
				r.mu.Lock()
				r.red = red
				r.mu.Unlock()
			}
		}
		r.mu.Lock()
		r.raw = append(r.raw, ev)
		shown := r.sh.show(ev, r.red)
		r.shown = append(r.shown, shown)
		for v := range r.viewers {
			r.send(v, shown)
		}
		r.mu.Unlock()
	}
	p.finish(r, Event{Kind: EventError, Text: "bộ não ngắt kết nối mà không báo kết thúc lượt"})
}

// send gửi không chờ; người xem đầy hàng đợi thì bị bỏ. Gọi khi giữ r.mu.
func (r *turnRun) send(v *viewer, ev Event) {
	select {
	case v.ch <- ev:
	default:
		r.drop(v)
	}
}

func (r *turnRun) drop(v *viewer) {
	if !v.closed {
		v.closed = true
		delete(r.viewers, v)
		close(v.ch)
	}
}

// finish ghi kết thúc của lượt vào DB TRƯỚC, rồi mới phát sự kiện cuối và gỡ
// lượt khỏi bộ nhớ: ai Subscribe sau lúc gỡ thì đọc DB và thấy đủ.
func (p *Panel) finish(r *turnRun, ev Event) {
	now := p.now()
	if ev.Time.IsZero() {
		ev.Time = now
	}
	r.mu.Lock()
	raw := append(slices.Clone(r.raw), ev)
	acc := r.sh.acc.String()
	r.mu.Unlock()

	end := store.TurnEnd{At: now}
	switch ev.Kind {
	case EventDone:
		// Như shower.show: chữ đã nối đủ hơn result của Claude Code.
		end.Status, end.Answer = store.TurnDone, acc
		if end.Answer == "" {
			end.Answer = ev.Text
		}
	case EventRefused:
		// Cửa kiểm từ chối phiên: không chữ nào của lượt được coi là câu trả lời.
		end.Status, end.Error = store.TurnRefused, ev.Text
	default:
		end.Status, end.Answer, end.Error = store.TurnError, acc, ev.Text
	}
	ctx := context.Background()
	if b, err := json.Marshal(raw); err == nil {
		end.Events = b
	}
	// Lỗi ghi ở đây không có ai để trả về (request đã xong từ lâu). Lượt còn
	// running thì Recover lúc serve khởi động lại sẽ đóng; ErrTurnNotRunning
	// nghĩa là Recover đã đóng trước.
	_ = p.Store.FinishAITurn(ctx, r.session, r.seq, end)
	if sess, err := p.Store.AISession(ctx, r.session); err == nil {
		sess.Last = now
		_ = p.Store.SaveAISessionState(ctx, sess)
	}

	p.st.mu.Lock()
	defer p.st.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.raw = raw
	shown := r.sh.show(ev, r.red)
	r.shown = append(r.shown, shown)
	for v := range r.viewers {
		r.send(v, shown)
		r.drop(v)
	}
	if p.st.runs[r.session] == r {
		delete(p.st.runs, r.session)
	}
}

func (p *Panel) runOf(sessionID string) *turnRun {
	p.st.mu.Lock()
	defer p.st.mu.Unlock()
	return p.st.runs[sessionID]
}

// ------------------------------------------------------ Subscribe, Session

// Subscribe: xem hợp đồng ở panel_types.go. ctx của request hết thì người xem
// tự được nhả.
func (p *Panel) Subscribe(ctx context.Context, sessionID string, seq int) (past []Event, live <-chan Event, cancel func(), ok bool, err error) {
	p.st.mu.Lock()
	if r := p.st.runs[sessionID]; r != nil && r.seq == seq {
		r.mu.Lock()
		past = slices.Clone(r.shown)
		v := &viewer{ch: make(chan Event, viewerBuffer)}
		r.viewers[v] = struct{}{}
		r.mu.Unlock()
		p.st.mu.Unlock()
		release := func() {
			r.mu.Lock()
			r.drop(v)
			r.mu.Unlock()
		}
		stop := context.AfterFunc(ctx, release)
		return past, v.ch, func() { stop(); release() }, true, nil
	}
	p.st.mu.Unlock()

	t, err := p.Store.AITurn(ctx, sessionID, seq)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, func() {}, false, nil
	}
	if err != nil {
		return nil, nil, func() {}, false, err
	}
	sess, err := p.Store.AISession(ctx, sessionID)
	if err != nil {
		return nil, nil, func() {}, false, err
	}
	past, _, err = storedView(t, NewRedactor(LevelAll, sess.Aliases))
	if err != nil {
		return nil, nil, func() {}, false, err
	}
	closed := make(chan Event)
	close(closed)
	return past, closed, func() {}, true, nil
}

func storedView(t store.AITurn, red *Redactor) ([]Event, string, error) {
	var raw []Event
	if len(t.Events) > 0 {
		if err := json.Unmarshal(t.Events, &raw); err != nil {
			return nil, "", fmt.Errorf("đọc sự kiện của lượt %s#%d: %w", t.SessionID, t.Seq, err)
		}
	}
	shown, acc := showEvents(raw, red)
	return shown, acc, nil
}

// Session: xem hợp đồng ở panel_types.go.
func (p *Panel) Session(ctx context.Context, id string) (SessionDetail, error) {
	sess, err := p.Store.AISession(ctx, id)
	if err != nil {
		return SessionDetail{}, err
	}
	turns, err := p.Store.AITurns(ctx, id)
	if err != nil {
		return SessionDetail{}, err
	}
	calls, err := p.Store.AICalls(ctx, id)
	if err != nil {
		return SessionDetail{}, err
	}
	plans, err := p.Store.PlansBySession(ctx, id)
	if err != nil {
		return SessionDetail{}, err
	}
	red := NewRedactor(LevelAll, sess.Aliases)
	d := SessionDetail{Session: sess, Turns: make([]TurnView, 0, len(turns)), Calls: calls, Plans: plans}
	run := p.runOf(id)
	for _, t := range turns {
		tv := TurnView{
			Seq: t.Seq, Lang: t.Lang, Question: t.Question, Chips: []Chip{}, Playbook: t.Playbook,
			Prompt: t.Prompt, Answer: red.Unalias(t.Answer), Status: t.Status, Error: t.Error, Started: t.Started,
		}
		if len(t.Context) > 0 {
			if err := json.Unmarshal(t.Context, &tv.Chips); err != nil {
				return SessionDetail{}, fmt.Errorf("đọc khối ngữ cảnh của lượt %s#%d: %w", id, t.Seq, err)
			}
		}
		if !t.Ended.IsZero() {
			e := t.Ended
			tv.Ended = &e
		}
		if run != nil && run.seq == t.Seq && t.Status == store.TurnRunning {
			// Lượt đang chạy: DB chưa có sự kiện, lấy ở bộ nhớ.
			run.mu.Lock()
			tv.Events = slices.Clone(run.shown)
			tv.Answer = run.red.Unalias(run.sh.acc.String())
			run.mu.Unlock()
		} else if tv.Events, _, err = storedView(t, red); err != nil {
			return SessionDetail{}, err
		}
		d.Turns = append(d.Turns, tv)
	}
	if d.Calls == nil {
		d.Calls = []store.AICall{}
	}
	if d.Plans == nil {
		d.Plans = []engine.PendingPlan{}
	}
	return d, nil
}

// Recover: xem hợp đồng ở panel_types.go.
func (p *Panel) Recover(ctx context.Context) error {
	_, err := p.Store.FailRunningAITurns(ctx,
		"tpan serve khởi động lại khi lượt này đang chạy; hãy hỏi lại", p.now())
	return err
}
