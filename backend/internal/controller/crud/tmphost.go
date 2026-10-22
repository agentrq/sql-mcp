package crud

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/model"
	"github.com/agentrq/sql-mcp/backend/internal/repository"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
)

var errTmphostNotConnected = errors.New("tmphost is not connected")

// TmphostCallbackPath is where tmphost sends a person back after approving.
const TmphostCallbackPath = "/api/v1/settings/tmphost/callback"

// tmphostFlowTTL is how long a person has to approve on tmphost.
const tmphostFlowTTL = 10 * time.Minute

// tmphostFlow is the half of a connect that waits in a sealed cookie while
// the person is on tmphost: the PKCE verifier never leaves this server
// unsealed, and the state ties the answer to the person who asked.
type tmphostFlow struct {
	UserID   int64  `json:"u"`
	State    string `json:"s"`
	Verifier string `json:"v"`
	Expires  int64  `json:"e"`
}

func (c *Controller) tmphostRedirectURI() string {
	return c.cfg.BaseURL + TmphostCallbackPath
}

// tmphostClientID is the client_id tmphost issued this server for its
// redirect URI, registering on first use. It is kept per redirect URI, so a
// change of APP_BASE_URL registers again.
func (c *Controller) tmphostClientID(ctx context.Context) (string, error) {
	key := "tmphost_client_id:" + c.tmphost.BaseURL() + ":" + c.tmphostRedirectURI()
	id, err := c.repo.GetSetting(ctx, key)
	if err != nil || id != "" {
		return id, err
	}
	id, err = c.tmphost.Register(ctx, c.tmphostRedirectURI(), "SQL MCP")
	if err != nil {
		return "", err
	}
	return id, c.repo.SetSetting(ctx, key, id)
}

// TmphostStatus says whether a person connected tmphost.
func (c *Controller) TmphostStatus(ctx context.Context, userID int64) (*entity.TmphostStatus, error) {
	st := &entity.TmphostStatus{ServerURL: c.tmphost.BaseURL()}
	conn, err := c.repo.GetTmphost(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	st.Connected, st.Account, st.ConnectedAt = true, conn.Account, conn.CreatedAt
	return st, nil
}

// TmphostConnect starts connecting tmphost: the URL to send the person to,
// and the sealed flow to keep in their cookie until they come back.
func (c *Controller) TmphostConnect(ctx context.Context, userID int64) (authURL, flow string, err error) {
	clientID, err := c.tmphostClientID(ctx)
	if err != nil {
		return "", "", err
	}
	f := tmphostFlow{UserID: userID, State: rand.Text(), Verifier: rand.Text() + rand.Text(), Expires: c.now().Add(tmphostFlowTTL).Unix()}
	raw, _ := json.Marshal(f)
	if flow, err = c.sealer.Seal(string(raw)); err != nil {
		return "", "", err
	}
	authURL, err = c.tmphost.AuthURL(ctx, clientID, c.tmphostRedirectURI(), f.State, auth.S256(f.Verifier))
	return authURL, flow, err
}

// TmphostCallbackRequest is what tmphost sent the person back with.
type TmphostCallbackRequest struct {
	UserID           int64
	Flow             string
	State            string
	Code             string
	Issuer           string
	Error            string
	ErrorDescription string
}

// TmphostCallback finishes connecting tmphost and stores the grant.
func (c *Controller) TmphostCallback(ctx context.Context, rq TmphostCallbackRequest) error {
	if rq.Error != "" {
		if rq.Error == "access_denied" {
			return entity.Invalid("tmphost access was denied")
		}
		return entity.Invalid(strings.TrimSpace("tmphost answered " + rq.Error + " " + rq.ErrorDescription))
	}
	raw, err := c.sealer.Open(rq.Flow)
	if err != nil {
		return entity.Invalid("the tmphost connection expired; connect again")
	}
	var f tmphostFlow
	if err := json.Unmarshal([]byte(raw), &f); err != nil || f.UserID != rq.UserID || f.State == "" || f.State != rq.State || c.now().Unix() > f.Expires {
		return entity.Invalid("the tmphost connection expired or was started elsewhere; connect again")
	}
	if err := c.tmphost.CheckIssuer(ctx, rq.Issuer); err != nil {
		return entity.Invalid(err.Error())
	}
	clientID, err := c.tmphostClientID(ctx)
	if err != nil {
		return err
	}
	token, err := c.tmphost.Exchange(ctx, clientID, c.tmphostRedirectURI(), rq.Code, f.Verifier)
	if err != nil {
		return entity.Invalid(fmt.Sprintf("tmphost refused the sign-in: %v", err))
	}
	account, err := c.tmphost.Account(ctx, token.AccessToken)
	if err != nil {
		account = ""
	}
	conn := &model.TmphostConnection{UserID: rq.UserID, Account: account, CreatedAt: c.now(), UpdatedAt: c.now()}
	if err := c.sealToken(conn, token.AccessToken, token.RefreshToken, token.ExpiresIn); err != nil {
		return err
	}
	return c.repo.SaveTmphost(ctx, conn)
}

func (c *Controller) sealToken(conn *model.TmphostConnection, access, refresh string, expiresIn int64) error {
	var err error
	if conn.AccessSealed, err = c.sealer.Seal(access); err != nil {
		return err
	}
	if refresh != "" {
		if conn.RefreshSealed, err = c.sealer.Seal(refresh); err != nil {
			return err
		}
	}
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	conn.ExpiresAt = c.now().Add(time.Duration(expiresIn) * time.Second)
	return nil
}

// DisconnectTmphost forgets a person's tmphost grant.
func (c *Controller) DisconnectTmphost(ctx context.Context, userID int64) error {
	return c.repo.DeleteTmphost(ctx, userID)
}

// tmphostToken returns a person's access token, refreshing it when it is
// about to expire.
func (c *Controller) tmphostToken(ctx context.Context, userID int64) (string, error) {
	conn, err := c.repo.GetTmphost(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", errTmphostNotConnected
	}
	if err != nil {
		return "", err
	}
	if c.now().Add(time.Minute).Before(conn.ExpiresAt) {
		return c.sealer.Open(conn.AccessSealed)
	}
	if conn.RefreshSealed == "" {
		return "", errors.New("the tmphost token expired; connect tmphost again")
	}
	refresh, err := c.sealer.Open(conn.RefreshSealed)
	if err != nil {
		return "", err
	}
	clientID, err := c.tmphostClientID(ctx)
	if err != nil {
		return "", err
	}
	token, err := c.tmphost.Refresh(ctx, clientID, refresh)
	if err != nil {
		return "", fmt.Errorf("the tmphost token could not be refreshed; connect tmphost again: %w", err)
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refresh
	}
	if err := c.sealToken(conn, token.AccessToken, token.RefreshToken, token.ExpiresIn); err != nil {
		return "", err
	}
	if err := c.repo.UpdateTmphostTokens(ctx, conn); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// storeResult uploads a result as CSV to a new 30-minute tmphost folder.
func (c *Controller) storeResult(ctx context.Context, userID int64, dbName string, columns []string, rows [][]any) (string, time.Time, error) {
	token, err := c.tmphostToken(ctx, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	body, err := ToCSV(columns, rows)
	if err != nil {
		return "", time.Time{}, err
	}
	folder, err := c.tmphost.CreateFolder(ctx, token, "SQL MCP: "+dbName, 0)
	if err != nil {
		return "", time.Time{}, err
	}
	file, err := c.tmphost.PutFile(ctx, token, folder.ID, "result-"+c.now().UTC().Format("20060102-150405")+".csv", body, "text/csv")
	if err != nil {
		return "", time.Time{}, err
	}
	return file.URL, folder.ExpiresAt, nil
}
