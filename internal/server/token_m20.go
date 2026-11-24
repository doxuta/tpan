package server

// Token API (M20, docs/thay-doi/M20-thiet-ke.md mục 3.7, 11.3, 11.5, 11.9,
// 11.12 R3, R9): xác thực bằng header Authorization ở guard, bảng nhóm của
// route (RouteTable), các route quản lý token (chỉ phiên).
//
// Quyền thật của một request bằng token = phạm vi của token ∩ quyền HIỆN TẠI
// của chủ token (vai đọc lại mỗi request): token không bao giờ làm được việc
// người tạo không làm được, kể cả sau khi người đó bị hạ vai hay bị khoá (B4).

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
)

// tokenCtxKey là khoá context giữ store.TokenRow của request đã qua guard
// bằng token. Request bằng token KHÔNG có authCtxKey (R3): handler chỉ phiên
// gọi sessionOnly thì nhận 401.
type tokenCtxKey struct{}

// tokenOf trả token của request (đã qua guard bằng token).
func tokenOf(ctx context.Context) (store.TokenRow, bool) {
	t, ok := ctx.Value(tokenCtxKey{}).(store.TokenRow)
	return t, ok
}

// tokenIDOf là id token của request; rỗng khi request dùng phiên (hay không
// qua guard). Nhật ký (audit.token_id) và mật khẩu hiện một lần của job dùng nó.
func tokenIDOf(ctx context.Context) string {
	t, _ := tokenOf(ctx)
	return t.ID
}

func tokenDenied(w http.ResponseWriter, code string) {
	msg := map[string]i18n.Text{
		"token_invalid":   i18n.T("Token không hợp lệ (không có, chủ token không còn hoặc đang bị khoá). Header phải là: Authorization: Bearer tpan_…", "Invalid token (unknown, or its owner is gone or suspended). The header must be: Authorization: Bearer tpan_…"),
		"token_malformed": i18n.T("Token sai dạng (thiếu hay thừa ký tự khi chép?).", "The token is malformed (characters lost or added when copying?)."),
		"token_revoked":   i18n.T("Token đã bị thu hồi.", "The token has been revoked."),
		"token_expired":   i18n.T("Token đã hết hạn. Tạo token mới ở trang Tài khoản.", "The token has expired. Create a new one in the Account page."),
	}[code]
	writeError(w, http.StatusUnauthorized, code, msg)
}

// tokenAuth xác thực token của request: thiếu "Bearer " → token_invalid; sai
// dạng → token_malformed (không chạm DB); không có → token_invalid; thu hồi →
// token_revoked; hết hạn → token_expired; chủ không còn hay không active →
// token_invalid. ok=false nghĩa là đã trả 401.
func (s *Server) tokenAuth(w http.ResponseWriter, r *http.Request) (store.TokenRow, account.Identity, bool) {
	st := s.deps.Store
	if st == nil {
		authNoStore(w)
		return store.TokenRow{}, account.Identity{}, false
	}
	raw, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found || len(r.Header.Values("Authorization")) != 1 {
		tokenDenied(w, "token_invalid")
		return store.TokenRow{}, account.Identity{}, false
	}
	raw = strings.TrimSpace(raw)
	if !apitoken.WellFormed(raw) {
		tokenDenied(w, "token_malformed")
		return store.TokenRow{}, account.Identity{}, false
	}
	t, err := st.TokenByHash(r.Context(), apitoken.Hash(raw))
	if errors.Is(err, store.ErrNotFound) {
		tokenDenied(w, "token_invalid")
		return store.TokenRow{}, account.Identity{}, false
	}
	if err != nil {
		authInternal(w, "đọc token", err)
		return store.TokenRow{}, account.Identity{}, false
	}
	now := s.now()
	switch {
	case !t.Revoked.IsZero():
		tokenDenied(w, "token_revoked")
		return store.TokenRow{}, account.Identity{}, false
	case !now.Before(t.Expires):
		tokenDenied(w, "token_expired")
		return store.TokenRow{}, account.Identity{}, false
	}
	u, found, err := st.User(r.Context(), t.User)
	if err != nil {
		authInternal(w, "đọc user", err)
		return store.TokenRow{}, account.Identity{}, false
	}
	if !found || u.Name == "" || u.Status != account.Active {
		tokenDenied(w, "token_invalid")
		return store.TokenRow{}, account.Identity{}, false
	}
	// Ghi lần dùng tối đa mỗi phút một lần (như phiên, authTouchEvery).
	if now.Sub(t.LastUsed) > authTouchEvery {
		if err := st.TouchToken(r.Context(), t.ID, clientIP(r), now); err != nil {
			slog.Error("ghi lần dùng token", "id", t.ID, "err", err)
		}
	}
	// Vai HIỆN TẠI của chủ, không có ViewedBy (token không "xem như" được).
	return t, account.Identity{Name: u.Name, Role: u.Role}, true
}

// guardToken là nhánh token của guard (routes.go): xác thực, nhóm của route,
// phạm vi, chính sách bắt buộc, rồi luật vai như phiên.
func (s *Server) guardToken(w http.ResponseWriter, r *http.Request, rt route) {
	t, id, ok := s.tokenAuth(w, r)
	if !ok {
		return
	}
	if rt.tok == "" {
		writeError(w, http.StatusForbidden, "token_forbidden", i18n.T(
			"Việc này chỉ dùng được khi đăng nhập trên giao diện, không dùng được bằng token.",
			"This route can only be used when signed in to the interface, not with a token."))
		return
	}
	scopes, err := apitoken.ParseScopes(strings.Split(t.Scopes, ","))
	if err != nil {
		slog.Error("phạm vi của token hỏng", "id", t.ID, "err", err)
		tokenDenied(w, "token_invalid")
		return
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	if !scopes.Allows(rt.tok, write) {
		need := string(rt.tok) + ":read"
		if write {
			need = string(rt.tok) + ":write"
		}
		writeError(w, http.StatusForbidden, "token_scope", i18n.T(
			"Token thiếu phạm vi "+need+".", "The token lacks the scope "+need+"."))
		return
	}
	if req, err := s.enrollRequired(r.Context(), id.Name, id.Role); err != nil {
		authInternal(w, "đọc chính sách hai bước", err)
		return
	} else if req && !rt.allowEnroll {
		writeEnrollRequired(w)
		return
	}
	ctx := context.WithValue(r.Context(), tokenCtxKey{}, t)
	ctx = context.WithValue(ctx, identityKey{}, id)
	s.serveRule(w, r.WithContext(engine.WithActor(ctx, id.Name)), rt, id)
}

// ---------------------------------------------------------------- bảng route

// RouteInfo là một dòng của bảng route cho script e2e và test (mục 11.10).
type RouteInfo struct {
	Pattern string
	// Rule: "public", "any", "admin", "owner:site|job|incident|backup|db".
	Rule string
	// TokenGroup: nhóm token của route; rỗng là chỉ phiên.
	TokenGroup string
}

// RouteTable trả bảng route thật của server (không có handler).
func RouteTable() []RouteInfo {
	rs := newServer(Deps{}).routes(nil)
	out := make([]RouteInfo, len(rs))
	for i, rt := range rs {
		out[i] = RouteInfo{Pattern: rt.pattern, Rule: rt.rule.String(), TokenGroup: string(rt.tok)}
	}
	return out
}

func (r rule) String() string {
	switch r.kind {
	case rulePublic:
		return "public"
	case ruleAny:
		return "any"
	case ruleAdmin:
		return "admin"
	case ruleOwner:
		return "owner:" + map[ownerObj]string{objSite: "site", objJob: "job", objIncident: "incident", objBackup: "backup", objDB: "db"}[r.obj]
	}
	return "unset"
}

// ---------------------------------------------------------------- route quản lý token

// tokenOut là dòng token cho API (không bao giờ có token thật).
func tokenOut(t store.TokenRow, now time.Time) TokenRow {
	status := "active"
	switch {
	case !t.Revoked.IsZero():
		status = "revoked"
	case !now.Before(t.Expires):
		status = "expired"
	}
	scopes := []string{}
	if t.Scopes != "" {
		scopes = strings.Split(t.Scopes, ",")
	}
	return TokenRow{
		ID: t.ID, User: t.User, Name: t.Name, Scopes: scopes, Hint: t.Hint,
		Created: t.Created, Expires: t.Expires, Revoked: t.Revoked, LastUsed: t.LastUsed, LastIP: t.LastIP,
		Status: status,
	}
}

func (s *Server) tokenList(ctx context.Context, user string) ([]TokenRow, error) {
	list, err := s.deps.Store.Tokens(ctx, user)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]TokenRow, len(list))
	for i, t := range list {
		out[i] = tokenOut(t, now)
	}
	return out, nil
}

// handleMyTokens: GET /api/v1/account/tokens.
func (s *Server) handleMyTokens(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	list, err := s.tokenList(r.Context(), cur.UserName)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	days, err := s.tokenMaxDays(r.Context())
	if err != nil {
		apiFail(w, r, err)
		return
	}
	isAdmin := realRole(who(r)) == account.RoleAdmin
	writeJSON(w, http.StatusOK, MyTokens{Tokens: list, MaxDays: days, Groups: groupNames(apitoken.GroupsFor(isAdmin))})
}

// handleTokenCreate: POST /api/v1/account/tokens. Kiểm đầu vào trước (rẻ, và
// không phí một mã TOTP: mã đã dùng thì phải chờ 30 giây), rồi mật khẩu, rồi
// mã hai bước khi đã bật: phiên bị chiếm không tự in được token sống lâu.
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	var req TokenCreate
	if !authDecode(w, r, &req) {
		return
	}
	ctx, user := r.Context(), cur.UserName
	const action = "token.create"
	invalid := func(err error) {
		var ie *apitoken.InvalidError
		if errors.As(err, &ie) {
			writeError(w, http.StatusBadRequest, ie.Code, i18n.T(ie.Vi, ie.En))
			return
		}
		apiFail(w, r, err)
	}
	scopes, err := apitoken.ParseScopes(req.Scopes)
	if err != nil {
		invalid(err)
		return
	}
	maxDays, err := s.tokenMaxDays(ctx)
	if err != nil {
		apiFail(w, r, err)
		return
	}
	if err := apitoken.CheckNew(realRole(who(r)) == account.RoleAdmin, req.Name, scopes, req.Days, maxDays); err != nil {
		invalid(err)
		return
	}
	needCode, ok := s.mfaCodeNeeded(w, r, user, req.Code)
	if !ok {
		return
	}
	if !s.accountPassword(w, r, user, req.Password, action) {
		return
	}
	if needCode {
		if _, ok := s.checkSecondFactor(w, r, user, req.Code, sfAccount(action)); !ok {
			return
		}
	}
	raw := apitoken.New()
	now := s.now()
	row := store.TokenRow{
		ID: hexID(8), User: user, Name: req.Name, Scopes: scopes.String(), Hint: apitoken.Hint(raw),
		Created: now, Expires: now.Add(time.Duration(req.Days) * 24 * time.Hour),
	}
	if !s.auditDone(w, r, action, row.ID, s.deps.Store.CreateToken(ctx, row, apitoken.Hash(raw))) {
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, TokenCreated{Token: raw, Row: tokenOut(row, now)})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request, user string) {
	id := r.PathValue("id")
	err := s.deps.Store.RevokeToken(r.Context(), id, user, s.now())
	if errors.Is(err, store.ErrNotFound) {
		s.audit(r, "token.revoke", id, "not_found", "")
		writeError(w, http.StatusNotFound, "token_not_found", i18n.T(
			"Không có token này (hoặc đã thu hồi).", "No such token (or it is already revoked)."))
		return
	}
	if !s.auditDone(w, r, "token.revoke", id, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMyTokenRevoke: DELETE /api/v1/account/tokens/{id}: chỉ token của mình;
// của người khác ra cùng 404 như không có.
func (s *Server) handleMyTokenRevoke(w http.ResponseWriter, r *http.Request) {
	cur, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	s.revokeToken(w, r, cur.UserName)
}

// handleTokens: GET /api/v1/tokens (admin, chỉ phiên): token của mọi user.
func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	if _, ok := sessionOnly(w, r); !ok {
		return
	}
	list, err := s.tokenList(r.Context(), "")
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleTokenRevoke: DELETE /api/v1/tokens/{id} (admin, chỉ phiên).
func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if _, ok := sessionOnly(w, r); !ok {
		return
	}
	s.revokeToken(w, r, "")
}
