package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Collections/Agents/codex-gateway/internal/auth"
)

type Config struct {
	ClientID          string
	ClientSecret      string
	AuthorizeEndpoint string
	TokenEndpoint     string
	RedirectHost      string
	RedirectPort      int
	RedirectPath      string
	Originator        string
	Scopes            []string
	Audience          string
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

type Client struct {
	cfg        Config
	httpClient *http.Client
	now        func() time.Time
	listen     func(network, address string) (net.Listener, error)
	openURL    func(string) error
	timeout    time.Duration
}

func NewClient(cfg Config, opts ...Option) *Client {
	c := &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		now:        time.Now,
		listen:     net.Listen,
		openURL:    openURLDefault,
		timeout:    5 * time.Minute,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) AuthenticateWithCallback(ctx context.Context, out io.Writer) (auth.Token, error) {
	pkce, err := generatePKCE()
	if err != nil {
		return auth.Token{}, fmt.Errorf("generate pkce: %w", err)
	}

	state, err := randomBase64URL(32)
	if err != nil {
		return auth.Token{}, fmt.Errorf("generate oauth state: %w", err)
	}

	redirectPath := c.cfg.RedirectPath
	if redirectPath == "" {
		redirectPath = "/auth/callback"
	}
	if !strings.HasPrefix(redirectPath, "/") {
		redirectPath = "/" + redirectPath
	}

	redirectHost := c.cfg.RedirectHost
	if redirectHost == "" {
		redirectHost = "localhost"
	}

	ln, err := c.listen("tcp", net.JoinHostPort(redirectHost, strconv.Itoa(c.cfg.RedirectPort)))
	if err != nil {
		return auth.Token{}, fmt.Errorf("start oauth callback listener: %w", err)
	}
	defer ln.Close()

	actualPort := ln.Addr().(*net.TCPAddr).Port
	urlHost := redirectHost
	if urlHost == "0.0.0.0" || urlHost == "::" {
		urlHost = "localhost"
	}

	redirectURI := fmt.Sprintf("http://%s:%d%s", urlHost, actualPort, redirectPath)
	authorizeURL := c.buildAuthorizeURL(redirectURI, pkce, state)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	var once sync.Once

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != redirectPath {
				http.NotFound(w, r)
				return
			}
			if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
				desc := r.URL.Query().Get("error_description")
				if desc == "" {
					desc = oauthErr
				}
				once.Do(func() { errCh <- fmt.Errorf("oauth callback error: %s", desc) })
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("<!doctype html><html><body><h2>Authorization Failed</h2></body></html>"))
				return
			}
			if r.URL.Query().Get("state") != state {
				once.Do(func() { errCh <- fmt.Errorf("invalid oauth callback state") })
				return
			}
			code := strings.TrimSpace(r.URL.Query().Get("code"))
			if code == "" {
				once.Do(func() { errCh <- fmt.Errorf("missing oauth authorization code") })
				return
			}
			once.Do(func() { codeCh <- code })
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><body><h2>Authorization Successful</h2><p>You can close this window.</p></body></html>`))
		}),
	}

	serveErrCh := make(chan error, 1)
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
		}
		close(serveErrCh)
	}()

	if out != nil {
		fmt.Fprintf(out, "Open this URL to login:\n  %s\n", authorizeURL)
		fmt.Fprintf(out, "Waiting for callback on %s ...\n", redirectURI)
	}
	if err := c.openURL(authorizeURL); err != nil && out != nil {
		fmt.Fprintf(out, "Could not open browser automatically. Please open the URL manually.\n")
	}

	timer := time.NewTimer(c.timeout)
	defer timer.Stop()

	var authCode string
	select {
	case authCode = <-codeCh:
	case err := <-errCh:
		_ = shutdownServer(server)
		return auth.Token{}, err
	case err := <-serveErrCh:
		if err != nil {
			return auth.Token{}, fmt.Errorf("oauth callback server failed: %w", err)
		}
		return auth.Token{}, fmt.Errorf("oauth callback server stopped unexpectedly")
	case <-timer.C:
		_ = shutdownServer(server)
		return auth.Token{}, fmt.Errorf("oauth callback timeout")
	case <-ctx.Done():
		_ = shutdownServer(server)
		return auth.Token{}, ctx.Err()
	}

	_ = shutdownServer(server)
	return c.exchangeCode(ctx, authCode, redirectURI, pkce.Verifier)
}

func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (auth.Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.cfg.ClientID},
	}
	if c.cfg.ClientSecret != "" {
		form.Set("client_secret", c.cfg.ClientSecret)
	}
	return c.postToken(ctx, form)
}

func (c *Client) exchangeCode(ctx context.Context, code, redirectURI, codeVerifier string) (auth.Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {c.cfg.ClientID},
		"code_verifier": {codeVerifier},
	}
	if c.cfg.ClientSecret != "" {
		form.Set("client_secret", c.cfg.ClientSecret)
	}
	token, err := c.postToken(ctx, form)
	if err != nil {
		return auth.Token{}, err
	}
	if token.AccessToken == "" {
		return auth.Token{}, fmt.Errorf("token exchange returned empty access token")
	}
	return token, nil
}

func (c *Client) postToken(ctx context.Context, form url.Values) (auth.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return auth.Token{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return auth.Token{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return auth.Token{}, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var oauthErr struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(b, &oauthErr)
		bodySnippet := strings.TrimSpace(string(b))
		const maxBodyLog = 2048
		if len(bodySnippet) > maxBodyLog {
			bodySnippet = bodySnippet[:maxBodyLog] + "...(truncated)"
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "token request failed: HTTP %d", resp.StatusCode)
		if oauthErr.Error != "" {
			fmt.Fprintf(&sb, ", oauth_error=%q", oauthErr.Error)
		}
		if oauthErr.ErrorDescription != "" {
			fmt.Fprintf(&sb, ", oauth_error_description=%q", oauthErr.ErrorDescription)
		}
		if bodySnippet != "" {
			fmt.Fprintf(&sb, ", response_body=%q", bodySnippet)
		}
		return auth.Token{}, errors.New(sb.String())
	}

	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	_ = json.Unmarshal(b, &tr)

	expiresAt := time.Time{}
	if tr.ExpiresIn > 0 {
		expiresAt = c.now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}

	return auth.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		Scope:        tr.Scope,
		ExpiresAt:    expiresAt,
	}, nil
}

func (c *Client) buildAuthorizeURL(redirectURI string, pkce pkceCodes, state string) string {
	params := url.Values{
		"response_type":              {"code"},
		"client_id":                  {c.cfg.ClientID},
		"redirect_uri":              {redirectURI},
		"scope":                     {strings.Join(c.cfg.Scopes, " ")},
		"code_challenge":            {pkce.Challenge},
		"code_challenge_method":     {"S256"},
		"state":                     {state},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow": {"true"},
		"originator":                {c.cfg.Originator},
	}
	return c.cfg.AuthorizeEndpoint + "?" + params.Encode()
}

// --- helpers ---

type pkceCodes struct {
	Verifier  string
	Challenge string
}

func generatePKCE() (pkceCodes, error) {
	verifier, err := randomBase64URL(32)
	if err != nil {
		return pkceCodes{}, err
	}
	hash := sha256.Sum256([]byte(verifier))
	return pkceCodes{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(hash[:]),
	}, nil
}

func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func shutdownServer(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func openURLDefault(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
