package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/hashicorp/vault/api"
)

// UserpassLogin opens a browser for the Vault username, password, and authenticator code.
// authMount is the userpass mount, usually "userpass". It returns a client token.
func UserpassLogin(client *api.Client, authMount string) (string, error) {
	authMount = strings.Trim(authMount, "/")
	if authMount == "" {
		return "", errors.New("userpass auth mount is required")
	}

	state, err := randomState()
	if err != nil {
		return "", err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen for Vault login: %w", err)
	}
	defer ln.Close()

	page := fmt.Sprintf("http://%s/", ln.Addr().String())
	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			writeLoginPage(w, loginForm{State: state})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			writeLoginPage(w, loginForm{State: state, Error: "Could not read the login form."})
			return
		}
		if r.Form.Get("state") != state {
			http.Error(w, "login session mismatch", http.StatusBadRequest)
			return
		}
		token, err := login(r.Context(), client, authMount, r.Form.Get("username"), r.Form.Get("password"), r.Form.Get("otp"))
		if err != nil {
			writeLoginPage(w, loginForm{State: state, Error: err.Error()})
			return
		}
		_, _ = w.Write([]byte("<html><body><p>Authenticated. You can close this tab and return to the terminal.</p></body></html>"))
		done <- result{token: token}
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	fmt.Fprintf(os.Stderr, "Opening browser for Vault login (%s).\nIf it does not open, visit:\n%s\n", client.Address(), page)
	if err := openBrowser(page); err != nil {
		fmt.Fprintf(os.Stderr, "could not open a browser: %v\n", err)
	}

	select {
	case res := <-done:
		return res.token, res.err
	case <-time.After(3 * time.Minute):
		return "", fmt.Errorf("timed out waiting for browser login")
	}
}

func login(ctx context.Context, client *api.Client, authMount, username, password, otp string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return "", errors.New("username and password are required")
	}

	secret, err := client.Logical().WriteWithContext(ctx, fmt.Sprintf("auth/%s/login/%s", authMount, url.PathEscape(username)), map[string]interface{}{
		"password": password,
	})
	if err != nil {
		return "", fmt.Errorf("userpass login: %w", err)
	}
	if secret == nil || secret.Auth == nil {
		return "", errors.New("userpass login did not return auth data")
	}
	if secret.Auth.ClientToken != "" && secret.Auth.MFARequirement == nil {
		return secret.Auth.ClientToken, nil
	}
	if secret.Auth.MFARequirement == nil {
		return "", errors.New("userpass login did not return a token")
	}

	methodID, err := totpMethodID(secret.Auth.MFARequirement)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(otp) == "" {
		return "", errors.New("authenticator code is required")
	}

	validated, err := client.Sys().MFAValidateWithContext(ctx, secret.Auth.MFARequirement.MFARequestID, map[string]interface{}{
		methodID: []string{strings.TrimSpace(otp)},
	})
	if err != nil {
		return "", fmt.Errorf("authenticator validation: %w", err)
	}
	if validated == nil || validated.Auth == nil || validated.Auth.ClientToken == "" {
		return "", errors.New("authenticator validation did not return a token")
	}
	return validated.Auth.ClientToken, nil
}

func totpMethodID(req *api.MFARequirement) (string, error) {
	for _, constraint := range req.MFAConstraints {
		if constraint == nil {
			continue
		}
		for _, method := range constraint.Any {
			if method != nil && method.ID != "" {
				return method.ID, nil
			}
		}
	}
	return "", errors.New("login did not include an authenticator method")
}

func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func openBrowser(rawURL string) error {
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		return fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
	return cmd.Start()
}

type loginForm struct {
	State string
	Error string
}

func writeLoginPage(w http.ResponseWriter, form loginForm) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginTemplate.Execute(w, form)
}

var loginTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Vault login</title>
</head>
<body>
  <h1>Sign in to Vault</h1>
  {{if .Error}}<p>{{.Error}}</p>{{end}}
  <form method="post" action="/">
    <input type="hidden" name="state" value="{{.State}}">
    <p><label>Username<br><input name="username" autocomplete="username" required></label></p>
    <p><label>Password<br><input name="password" type="password" autocomplete="current-password" required></label></p>
    <p><label>Authenticator code<br><input name="otp" inputmode="numeric" autocomplete="one-time-code"></label></p>
    <button type="submit">Sign in</button>
  </form>
</body>
</html>
`))
