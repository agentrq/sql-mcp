// Package api is the REST API under /api/v1, on Fiber. The browser
// authenticates with the HttpOnly `at` cookie; `rt`, scoped to the refresh
// route, renews it. The sqlmcp CLI sends its own token as a Bearer, which
// opens only the routes registered with cli, and only the databases it was
// granted.
package api

import (
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog/log"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/model"
	"github.com/agentrq/sql-mcp/backend/internal/data/view"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
	"github.com/agentrq/sql-mcp/backend/internal/service/security"
)

const (
	// BasePath is where the API is mounted.
	BasePath = "/api/v1"

	accessCookie      = "at"
	refreshCookie     = "rt"
	tmphostFlowCookie = "tmphost_flow"
	localUserID       = "userID"
	localCLIGrant     = "cliGrant"
)

// Params is what the API needs.
type Params struct {
	Router       fiber.Router
	Crud         *crud.Controller
	Tokens       *auth.TokenService
	Google       auth.Provider // nil when Google sign-in is not configured
	BaseURL      string
	CookieSecure bool
	RootLogin    bool
	RootToken    string
	RootEmail    string
}

type handler struct {
	crud         *crud.Controller
	tokens       *auth.TokenService
	google       auth.Provider
	baseURL      string
	cookieSecure bool
	rootLogin    bool
	rootToken    string
	rootEmail    string
}

// Register mounts the API on p.Router. Registration order is what makes a
// route public: everything after the auth middleware needs a session.
func Register(p Params) {
	h := &handler{
		crud: p.Crud, tokens: p.Tokens, google: p.Google, baseURL: p.BaseURL, cookieSecure: p.CookieSecure,
		rootLogin: p.RootLogin, rootToken: p.RootToken, rootEmail: p.RootEmail,
	}
	r := p.Router

	pub := r.Group("/auth")
	pub.Get("/config", h.authConfig)
	pub.Get("/google/login", h.googleLogin)
	pub.Get("/google/callback", h.googleCallback)
	pub.Post("/root/login", h.rootLoginHandler)
	// Public on purpose: the access token has expired by the time it is
	// called. It authenticates with the refresh cookie instead.
	pub.Post("/refresh", h.refresh)

	r.Use(h.requireSession)

	// Every route is either the browser's (web) or also the CLI's (cli).
	// The CLI reads, queries and adds PII columns; it never removes one or
	// changes a database.
	r.Get("/auth/user", h.cli(h.user))
	r.Post("/auth/logout", h.web(h.logout))

	r.Get("/drivers", h.cli(h.drivers))
	r.Get("/databases", h.cli(h.listDatabases))
	r.Post("/databases", h.web(h.createDatabase))
	r.Post("/databases/test", h.web(h.testConnection))
	r.Get("/databases/:id", h.cli(h.getDatabase))
	r.Put("/databases/:id", h.web(h.updateDatabase))
	r.Delete("/databases/:id", h.web(h.deleteDatabase))
	r.Get("/databases/:id/schema", h.cli(h.schema))
	r.Get("/databases/:id/pii", h.cli(h.getPII))
	r.Post("/databases/:id/pii", h.cli(h.addPII))
	r.Put("/databases/:id/pii", h.web(h.setPII))
	r.Put("/databases/:id/pii/agent", h.web(h.setAgentPII))
	r.Post("/databases/:id/query", h.cli(h.query))
	r.Get("/databases/:id/queries", h.cli(h.queries))

	r.Get("/settings/tmphost", h.web(h.tmphostStatus))
	r.Get("/settings/tmphost/connect", h.web(h.tmphostConnect))
	r.Get("/settings/tmphost/callback", h.web(h.tmphostCallback))
	r.Delete("/settings/tmphost", h.web(h.tmphostDisconnect))
}

// ── Errors ────────────────────────────────────────────────────────────────

func fail(c fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(view.Error{Error: view.ErrorBody{Message: message, Code: status}})
}

// respondErr maps a controller error to a status. An unexpected error is
// logged and answered without its text, which may name internals.
func respondErr(c fiber.Ctx, err error) error {
	var invalid *entity.InvalidError
	var rejected *entity.RejectedError
	var failed *entity.QueryFailedError
	switch {
	case errors.Is(err, entity.ErrNotFound):
		return fail(c, fiber.StatusNotFound, "not found")
	case errors.Is(err, entity.ErrEmailNotAllowed):
		return fail(c, fiber.StatusForbidden, err.Error())
	case errors.As(err, &invalid):
		return fail(c, fiber.StatusBadRequest, invalid.Message)
	case errors.As(err, &rejected):
		return fail(c, fiber.StatusUnprocessableEntity, rejected.Reason)
	case errors.As(err, &failed):
		return fail(c, fiber.StatusBadRequest, failed.Message)
	}
	log.Error().Err(err).Str("path", c.Path()).Msg("request failed")
	return fail(c, fiber.StatusInternalServerError, "internal server error")
}

func badPayload(c fiber.Ctx) error {
	return fail(c, fiber.StatusBadRequest, "invalid request payload")
}

// ── Sessions ──────────────────────────────────────────────────────────────

func (h *handler) cookie(name, value, path string, expires time.Time) *fiber.Cookie {
	return &fiber.Cookie{
		Name: name, Value: value, Path: path, Expires: expires,
		HTTPOnly: true, Secure: h.cookieSecure, SameSite: fiber.CookieSameSiteLaxMode,
	}
}

// issueSession sets both halves of a session.
func (h *handler) issueSession(c fiber.Ctx, u *entity.User) error {
	access, err := h.tokens.CreateAccessToken(u.ID, u.Email, u.Name, u.Picture)
	if err != nil {
		return err
	}
	refresh, err := h.tokens.CreateRefreshToken(u.ID)
	if err != nil {
		return err
	}
	now := time.Now()
	c.Cookie(h.cookie(accessCookie, access, "/", now.Add(auth.AccessTokenTTL)))
	c.Cookie(h.cookie(refreshCookie, refresh, BasePath+"/auth/refresh", now.Add(auth.RefreshTokenTTL)))
	return nil
}

// clearSession expires both halves, each at the path it was set on.
func (h *handler) clearSession(c fiber.Ctx) {
	past := time.Now().Add(-time.Hour)
	c.Cookie(h.cookie(accessCookie, "", "/", past))
	c.Cookie(h.cookie(refreshCookie, "", BasePath+"/auth/refresh", past))
}

func (h *handler) requireSession(c fiber.Ctx) error {
	if token, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer "); ok {
		grant, err := h.tokens.ValidateCLIAccessToken(token)
		id := crud.ParseID(grant.UserID)
		if err != nil || id == 0 {
			return fail(c, fiber.StatusUnauthorized, "unauthorized")
		}
		c.Locals(localUserID, id)
		c.Locals(localCLIGrant, &grant)
		return c.Next()
	}
	claims, err := h.tokens.ValidateAccessToken(c.Cookies(accessCookie))
	if err != nil {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	id := crud.ParseID(claims.Subject)
	if id == 0 {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	c.Locals(localUserID, id)
	return c.Next()
}

// cliGrant is the CLI's grant, or nil for the browser.
func cliGrant(c fiber.Ctx) *auth.Grant {
	g, _ := c.Locals(localCLIGrant).(*auth.Grant)
	return g
}

// granted reports whether the request may see database id: the browser
// sees every database of its person, the CLI only those it was granted.
func granted(c fiber.Ctx, id string) bool {
	g := cliGrant(c)
	return g == nil || slices.Contains(g.Databases, id)
}

// web keeps a route to the browser.
func (h *handler) web(next fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		if cliGrant(c) != nil {
			return fail(c, fiber.StatusForbidden, "not available to the CLI; use the web app")
		}
		return next(c)
	}
}

// cli opens a route to the CLI too. A database it was not granted is not
// found, like someone else's.
func (h *handler) cli(next fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		if id := c.Params("id"); id != "" && !granted(c, id) {
			return fail(c, fiber.StatusNotFound, "not found")
		}
		return next(c)
	}
}

func userID(c fiber.Ctx) int64 {
	id, _ := c.Locals(localUserID).(int64)
	return id
}

func (h *handler) authConfig(c fiber.Ctx) error {
	return c.JSON(view.AuthConfig{GoogleLoginEnabled: h.google != nil, RootLoginEnabled: h.rootLogin})
}

// safeRedirect keeps a post-sign-in redirect on this app: a same-origin path,
// or an absolute URL on BaseURL's origin. Anything else goes home.
func (h *handler) safeRedirect(raw string) string {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.HasPrefix(raw, "/\\") {
		return raw
	}
	if u, err := url.Parse(raw); err == nil && u.IsAbs() {
		if base, err := url.Parse(h.baseURL); err == nil && u.Scheme == base.Scheme && u.Host == base.Host {
			return raw
		}
	}
	return "/"
}

func (h *handler) googleLogin(c fiber.Ctx) error {
	if h.google == nil {
		return fail(c, fiber.StatusNotFound, "Google sign-in is not configured")
	}
	state, err := h.tokens.CreateStateToken("google", h.safeRedirect(c.Query("redirect_url", "/")))
	if err != nil {
		return respondErr(c, err)
	}
	return c.Redirect().Status(fiber.StatusFound).To(h.google.AuthURL(state))
}

func (h *handler) loginError(c fiber.Ctx, code string) error {
	return c.Redirect().Status(fiber.StatusFound).To("/login?error=" + url.QueryEscape(code))
}

func (h *handler) googleCallback(c fiber.Ctx) error {
	if h.google == nil {
		return fail(c, fiber.StatusNotFound, "Google sign-in is not configured")
	}
	redirect, err := h.tokens.ValidateStateToken("google", c.Query("state"))
	if err != nil {
		return h.loginError(c, "sign_in_expired")
	}
	if c.Query("error") != "" {
		return h.loginError(c, "sign_in_cancelled")
	}
	gu, err := h.google.Exchange(c.Context(), c.Query("code"))
	if err != nil {
		log.Warn().Err(err).Msg("Google sign-in failed")
		return h.loginError(c, "sign_in_failed")
	}
	u, err := h.crud.SignIn(c.Context(), gu.Email, gu.Name, gu.Picture)
	if errors.Is(err, entity.ErrEmailNotAllowed) {
		log.Info().Str("email", gu.Email).Msg("sign-in refused: email not on the allowlist")
		return h.loginError(c, "email_not_allowed")
	}
	if err != nil {
		return respondErr(c, err)
	}
	if err := h.issueSession(c, u); err != nil {
		return respondErr(c, err)
	}
	return c.Redirect().Status(fiber.StatusFound).To(h.safeRedirect(redirect))
}

func (h *handler) rootLoginHandler(c fiber.Ctx) error {
	if !h.rootLogin {
		return fail(c, fiber.StatusForbidden, "root login is disabled")
	}
	var rq view.RootLoginRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return badPayload(c)
	}
	if h.rootToken == "" || !security.SecureCompare(rq.RootToken, h.rootToken) {
		return fail(c, fiber.StatusUnauthorized, "invalid root token")
	}
	u, err := h.crud.SignIn(c.Context(), h.rootEmail, "Root Administrator", "")
	if err != nil {
		return respondErr(c, err)
	}
	if err := h.issueSession(c, u); err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.Status{Status: "ok"})
}

func (h *handler) refresh(c fiber.Ctx) error {
	claims, err := h.tokens.ValidateRefreshToken(c.Cookies(refreshCookie))
	if err != nil {
		h.clearSession(c)
		return fail(c, fiber.StatusUnauthorized, "session expired")
	}
	u, err := h.crud.GetUser(c.Context(), crud.ParseID(claims.Subject))
	if err != nil {
		// Gone, or no longer on the allowlist: either way the session ends.
		h.clearSession(c)
		return fail(c, fiber.StatusUnauthorized, "session expired")
	}
	if err := h.issueSession(c, u); err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.Status{Status: "ok"})
}

func (h *handler) user(c fiber.Ctx) error {
	u, err := h.crud.GetUser(c.Context(), userID(c))
	if err != nil {
		if errors.Is(err, entity.ErrEmailNotAllowed) || errors.Is(err, entity.ErrNotFound) {
			h.clearSession(c)
			return fail(c, fiber.StatusUnauthorized, "unauthorized")
		}
		return respondErr(c, err)
	}
	return c.JSON(view.FromUser(u))
}

func (h *handler) logout(c fiber.Ctx) error {
	h.clearSession(c)
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Databases ────────────────────────────────────────────────────────────

func dbID(c fiber.Ctx) int64 { return crud.ParseID(c.Params("id")) }

func (h *handler) drivers(c fiber.Ctx) error {
	return c.JSON(view.FromDrivers(h.crud.Drivers()))
}

func (h *handler) listDatabases(c fiber.Ctx) error {
	dbs, err := h.crud.ListDatabases(c.Context(), userID(c))
	if err != nil {
		return respondErr(c, err)
	}
	dbs = slices.DeleteFunc(dbs, func(d entity.Database) bool { return !granted(c, d.ID) })
	return c.JSON(view.FromDatabases(dbs))
}

func (h *handler) createDatabase(c fiber.Ctx) error {
	var rq view.DatabaseRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return badPayload(c)
	}
	d, err := h.crud.CreateDatabase(c.Context(), userID(c), rq.Database.Name, rq.Database.Driver, rq.Database.DSN)
	if err != nil {
		return respondErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(view.DatabaseResponse{Database: view.FromDatabase(d)})
}

func (h *handler) testConnection(c fiber.Ctx) error {
	var rq view.DatabaseRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return badPayload(c)
	}
	version, err := h.crud.TestConnection(c.Context(), rq.Database.Driver, rq.Database.DSN)
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.TestConnectionResponse{OK: true, ServerVersion: version})
}

func (h *handler) getDatabase(c fiber.Ctx) error {
	d, err := h.crud.GetDatabase(c.Context(), userID(c), dbID(c))
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.DatabaseResponse{Database: view.FromDatabase(d)})
}

func (h *handler) updateDatabase(c fiber.Ctx) error {
	var rq view.DatabaseRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return badPayload(c)
	}
	d, err := h.crud.UpdateDatabase(c.Context(), userID(c), dbID(c), rq.Database.Name, rq.Database.DSN)
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.DatabaseResponse{Database: view.FromDatabase(d)})
}

func (h *handler) deleteDatabase(c fiber.Ctx) error {
	if err := h.crud.DeleteDatabase(c.Context(), userID(c), dbID(c)); err != nil {
		return respondErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) schema(c fiber.Ctx) error {
	tables, err := h.crud.Schema(c.Context(), userID(c), dbID(c))
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.FromSchema(tables))
}

func (h *handler) getPII(c fiber.Ctx) error {
	cols, err := h.crud.ListPII(c.Context(), userID(c), dbID(c))
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.FromPIIColumns(cols))
}

func (h *handler) setAgentPII(c fiber.Ctx) error {
	var rq view.AgentPIIRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return badPayload(c)
	}
	d, err := h.crud.SetAgentMarksPII(c.Context(), userID(c), dbID(c), rq.AgentMarksPII)
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.DatabaseResponse{Database: view.FromDatabase(d)})
}

func (h *handler) setPII(c fiber.Ctx) error {
	var rq view.PIIColumnsRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return badPayload(c)
	}
	cols, err := h.crud.SetPII(c.Context(), userID(c), dbID(c), view.ToPIIColumns(rq.PIIColumns))
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.FromPIIColumns(cols))
}

func (h *handler) addPII(c fiber.Ctx) error {
	var rq view.PIIColumnsRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return badPayload(c)
	}
	cols, err := h.crud.AddPII(c.Context(), userID(c), dbID(c), view.ToPIIColumns(rq.PIIColumns))
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.FromPIIColumns(cols))
}

func (h *handler) query(c fiber.Ctx) error {
	var rq view.QueryRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return badPayload(c)
	}
	// Neither source stores a result on tmphost: it comes back whole.
	source := model.SourceConsole
	if cliGrant(c) != nil {
		source = model.SourceCLI
	}
	res, err := h.crud.RunQuery(c.Context(), crud.QueryRequest{
		UserID: userID(c), DatabaseID: dbID(c), SQL: rq.SQL, Source: source,
	})
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.FromQueryResult(res))
}

func (h *handler) queries(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	logs, err := h.crud.ListQueries(c.Context(), userID(c), dbID(c), limit)
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.FromQueryLogs(logs))
}

// ── tmphost ───────────────────────────────────────────────────────────────

func (h *handler) tmphostStatus(c fiber.Ctx) error {
	st, err := h.crud.TmphostStatus(c.Context(), userID(c))
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.FromTmphostStatus(st))
}

func settingsRedirect(status, message string) string {
	q := url.Values{"tmphost": {status}}
	if message != "" {
		q.Set("message", message)
	}
	return "/settings?" + q.Encode()
}

// tmphostConnect is a browser navigation, not a fetch: it ends on tmphost's
// consent page.
func (h *handler) tmphostConnect(c fiber.Ctx) error {
	authURL, flow, err := h.crud.TmphostConnect(c.Context(), userID(c))
	if err != nil {
		log.Warn().Err(err).Msg("could not start connecting tmphost")
		return c.Redirect().Status(fiber.StatusFound).To(settingsRedirect("error", "tmphost could not be reached"))
	}
	c.Cookie(h.cookie(tmphostFlowCookie, flow, BasePath+"/settings/tmphost/callback", time.Now().Add(10*time.Minute)))
	return c.Redirect().Status(fiber.StatusFound).To(authURL)
}

func (h *handler) tmphostCallback(c fiber.Ctx) error {
	err := h.crud.TmphostCallback(c.Context(), crud.TmphostCallbackRequest{
		UserID:           userID(c),
		Flow:             c.Cookies(tmphostFlowCookie),
		State:            c.Query("state"),
		Code:             c.Query("code"),
		Issuer:           c.Query("iss"),
		Error:            c.Query("error"),
		ErrorDescription: c.Query("error_description"),
	})
	c.Cookie(h.cookie(tmphostFlowCookie, "", BasePath+"/settings/tmphost/callback", time.Now().Add(-time.Hour)))
	var invalid *entity.InvalidError
	switch {
	case errors.As(err, &invalid):
		return c.Redirect().Status(fiber.StatusFound).To(settingsRedirect("error", invalid.Message))
	case err != nil:
		log.Error().Err(err).Msg("could not finish connecting tmphost")
		return c.Redirect().Status(fiber.StatusFound).To(settingsRedirect("error", "tmphost could not be connected"))
	}
	return c.Redirect().Status(fiber.StatusFound).To(settingsRedirect("connected", ""))
}

func (h *handler) tmphostDisconnect(c fiber.Ctx) error {
	if err := h.crud.DisconnectTmphost(c.Context(), userID(c)); err != nil {
		return respondErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
