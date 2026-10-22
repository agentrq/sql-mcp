// Package admin is the setup API under /admin/api/v1, for whoever holds the
// root token. Signing in with it mints an admin JWT, its own audience and an
// hour long, sent back both as the HttpOnly `admin_at` cookie (scoped to
// /admin, for the web app) and in the body, for scripts that send it as a
// Bearer token. An admin is nobody's account: the token opens the setup and
// no one's databases.
package admin

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog/log"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/view"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
	"github.com/agentrq/sql-mcp/backend/internal/service/security"
)

const (
	// BasePath is where the admin API is mounted.
	BasePath = "/admin/api/v1"

	adminCookie = "admin_at"
	cookiePath  = "/admin"
)

// Server is what the setup page shows about how the server was started;
// none of it can be changed at runtime.
type Server struct {
	BaseURL            string
	SSLEnabled         bool
	Domain             string
	HTTPSPort          int
	GoogleLoginEnabled bool
	RootLoginEnabled   bool
	AppDBDriver        string
	TmphostAPIURL      string
}

// Params is what the admin API needs. With no RootToken it answers only
// that it is disabled.
type Params struct {
	Router       fiber.Router
	Crud         *crud.Controller
	Tokens       *auth.TokenService
	RootToken    string
	CookieSecure bool
	Server       Server
}

type handler struct {
	crud         *crud.Controller
	tokens       *auth.TokenService
	rootToken    string
	cookieSecure bool
	server       Server
}

// Register mounts the admin API on p.Router.
func Register(p Params) {
	h := &handler{crud: p.Crud, tokens: p.Tokens, rootToken: p.RootToken, cookieSecure: p.CookieSecure, server: p.Server}
	r := p.Router
	r.Get("/config", h.config)
	r.Post("/login", h.login)
	r.Post("/logout", h.logout)

	r.Use(h.requireAdmin)
	r.Get("/session", h.session)
	r.Get("/settings", h.getSettings)
	r.Put("/settings", h.putSettings)
	r.Delete("/settings", h.resetSettings)
	r.Post("/settings/check-email", h.checkEmail)
}

func fail(c fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(view.Error{Error: view.ErrorBody{Message: message, Code: status}})
}

func respondErr(c fiber.Ctx, err error) error {
	if invalid, ok := err.(*entity.InvalidError); ok {
		return fail(c, fiber.StatusBadRequest, invalid.Message)
	}
	log.Error().Err(err).Str("path", c.Path()).Msg("admin request failed")
	return fail(c, fiber.StatusInternalServerError, "internal server error")
}

func (h *handler) enabled() bool { return h.rootToken != "" }

func (h *handler) config(c fiber.Ctx) error {
	return c.JSON(view.AdminConfig{Enabled: h.enabled()})
}

func (h *handler) cookie(value string, expires time.Time) *fiber.Cookie {
	return &fiber.Cookie{
		Name: adminCookie, Value: value, Path: cookiePath, Expires: expires,
		HTTPOnly: true, Secure: h.cookieSecure, SameSite: fiber.CookieSameSiteStrictMode,
	}
}

func (h *handler) login(c fiber.Ctx) error {
	if !h.enabled() {
		return fail(c, fiber.StatusForbidden, "the admin setup is disabled: set ROOT_TOKEN to enable it")
	}
	var rq view.RootLoginRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return fail(c, fiber.StatusBadRequest, "invalid request payload")
	}
	if !security.SecureCompare(rq.RootToken, h.rootToken) {
		log.Warn().Str("ip", c.IP()).Msg("admin sign-in refused: wrong root token")
		return fail(c, fiber.StatusUnauthorized, "invalid root token")
	}
	token, expiresAt, err := h.tokens.CreateAdminToken()
	if err != nil {
		return respondErr(c, err)
	}
	c.Cookie(h.cookie(token, expiresAt))
	return c.JSON(view.AdminSession{Token: token, ExpiresAt: expiresAt})
}

func (h *handler) logout(c fiber.Ctx) error {
	c.Cookie(h.cookie("", time.Now().Add(-time.Hour)))
	return c.SendStatus(fiber.StatusNoContent)
}

// requireAdmin takes the token from the Authorization header, else the
// cookie. Without ROOT_TOKEN no token is accepted, so removing it from the
// environment ends every admin session at the next restart.
func (h *handler) requireAdmin(c fiber.Ctx) error {
	token, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	if !ok {
		token = c.Cookies(adminCookie)
	}
	claims, err := h.tokens.ValidateAdminToken(token)
	if !h.enabled() || err != nil {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	c.Locals("adminExpiresAt", claims.ExpiresAt.Time)
	return c.Next()
}

func (h *handler) session(c fiber.Ctx) error {
	expiresAt, _ := c.Locals("adminExpiresAt").(time.Time)
	return c.JSON(view.AdminSession{ExpiresAt: expiresAt})
}

func (h *handler) settings(s entity.Setup) view.AdminSettings {
	return view.FromSetup(s, view.AdminServer(h.server))
}

func (h *handler) getSettings(c fiber.Ctx) error {
	return c.JSON(h.settings(h.crud.Setup()))
}

func (h *handler) putSettings(c fiber.Ctx) error {
	var rq view.AdminSettingsRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return fail(c, fiber.StatusBadRequest, "invalid request payload")
	}
	s, err := h.crud.UpdateSetup(c.Context(), rq.ToSetup())
	if err != nil {
		return respondErr(c, err)
	}
	log.Info().Int("allowedEmails", len(s.AllowedEmails)).Msg("admin saved the configuration")
	return c.JSON(h.settings(s))
}

func (h *handler) resetSettings(c fiber.Ctx) error {
	s, err := h.crud.ResetSetup(c.Context())
	if err != nil {
		return respondErr(c, err)
	}
	log.Info().Msg("admin reset the configuration to the environment's")
	return c.JSON(h.settings(s))
}

func (h *handler) checkEmail(c fiber.Ctx) error {
	var rq view.CheckEmailRequest
	if err := c.Bind().JSON(&rq); err != nil {
		return fail(c, fiber.StatusBadRequest, "invalid request payload")
	}
	match, err := crud.CheckEmail(rq.AllowedEmails, rq.Email)
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(view.CheckEmailResponse{Allowed: match != "", MatchedEntry: match})
}
