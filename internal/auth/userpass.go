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

// UserpassLogin opens a browser for the Vault username and password.
// An authenticator code is sent only when the user enters one.
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
			writeLoginPage(w, loginForm{State: state, Address: client.Address(), AuthMount: authMount})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			writeLoginPage(w, loginForm{State: state, Address: client.Address(), AuthMount: authMount, Error: "Could not read the login form."})
			return
		}
		if r.Form.Get("state") != state {
			http.Error(w, "login session mismatch", http.StatusBadRequest)
			return
		}
		username := r.Form.Get("username")
		token, err := login(r.Context(), client, authMount, username, r.Form.Get("password"), r.Form.Get("otp"))
		if err != nil {
			writeLoginPage(w, loginForm{State: state, Address: client.Address(), AuthMount: authMount, Username: strings.TrimSpace(username), Error: loginMessage(err)})
			return
		}
		writeLoginPage(w, loginForm{Address: client.Address(), AuthMount: authMount, Done: true})
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

	fmt.Fprintf(os.Stderr, "Sign in to %s\nIf the browser does not open, visit:\n%s\n", client.Address(), page)
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
	otp = strings.TrimSpace(otp)
	if otp == "" || secret.Auth.MFARequirement == nil {
		if secret.Auth.ClientToken == "" {
			return "", errors.New("userpass login did not return a token")
		}
		return secret.Auth.ClientToken, nil
	}

	methodID, err := totpMethodID(secret.Auth.MFARequirement)
	if err != nil {
		return "", err
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
	State     string
	Address   string
	Host      string
	AuthMount string
	Username  string
	Error     string
	Done      bool
}

func loginMessage(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid username or password"):
		return "Username or password was rejected."
	case strings.Contains(msg, "authenticator"):
		return "Authenticator code was rejected."
	case strings.Contains(msg, "username and password are required"):
		return "Username and password are required."
	default:
		return "Vault did not accept this sign-in."
	}
}

func writeLoginPage(w http.ResponseWriter, form loginForm) {
	form.Address = strings.TrimRight(strings.TrimSpace(form.Address), "/")
	if u, err := url.Parse(form.Address); err == nil && u.Host != "" {
		form.Host = u.Host
	} else {
		form.Host = form.Address
	}
	form.AuthMount = strings.Trim(strings.TrimSpace(form.AuthMount), "/")
	if form.AuthMount == "" {
		form.AuthMount = "userpass"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = loginTemplate.Execute(w, form)
}

var loginTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Sign in to {{.Host}}</title>
  <style>
    :root {
      color-scheme: light;
      --canvas: #f8fafc;
      --card: #ffffff;
      --ink: #0f172a;
      --body: #334155;
      --muted: #64748b;
      --faint: #94a3b8;
      --danger: #93000a;
      --danger-bg: #ffdad6;
    }
    * { box-sizing: border-box; }
    html, body { margin: 0; min-height: 100%; }
    body {
      background: var(--canvas);
      color: var(--ink);
      font: 14px/1.45 Inter, ui-sans-serif, system-ui, "Segoe UI", sans-serif;
    }
    main {
      width: min(440px, calc(100% - 32px));
      min-height: 100vh;
      margin: 0 auto;
      padding: 32px 0 18vh;
      display: flex;
      flex-direction: column;
      justify-content: center;
    }
    .card {
      background: var(--card);
      border: 1px solid #e2e8f0;
      border-radius: 6px;
      box-shadow: 0 1px 3px rgba(15, 23, 42, 0.05), 0 1px 2px -1px rgba(15, 23, 42, 0.05);
    }
    .badge {
      border: 1px solid #e2e8f0;
      border-radius: 4px;
      background: #fff;
      color: #475569;
      font-size: 11px;
      font-weight: 500;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      padding: 2px 6px;
    }
    .meta { display: grid; gap: 6px; margin: 0 0 20px; }
    .meta div { display: flex; justify-content: space-between; gap: 16px; font-size: 13px; }
    .meta dt { color: var(--muted); font-weight: 500; }
    .meta dd { margin: 0; color: #111111; font-weight: 600; text-align: right; word-break: break-all; }
    .form-card { width: 100%; padding: 16px; }
    .heading { display: flex; align-items: center; gap: 8px; margin: 0 0 14px; }
    .heading .badge { margin-left: auto; }
    h1 { margin: 0; font-size: 18px; line-height: 26px; letter-spacing: -0.015em; }
    .lead { margin: 0 0 20px; color: #475569; font-size: 12px; line-height: 1.5; }
    .error {
      margin: 0 0 16px;
      padding: 10px 12px;
      border-radius: 4px;
      background: var(--danger-bg);
      color: var(--danger);
      font-size: 13px;
    }
    form, .done { display: grid; gap: 16px; }
    .field { display: grid; gap: 6px; }
    .field-label { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
    label { color: #334155; font-size: 12px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; }
    .hint { color: var(--faint); font-size: 11px; font-weight: 500; }
    .optional {
      color: #475569;
      background: #f1f5f9;
      border: 1px solid #e2e8f0;
      border-radius: 4px;
      padding: 1px 6px;
      letter-spacing: 0;
      text-transform: none;
    }
    .control {
      display: flex;
      align-items: center;
      width: 100%;
      height: 40px;
      border: 1px solid #cbd5e1;
      border-radius: 4px;
      background: #fff;
      overflow: hidden;
    }
    .control:focus-within { border-color: #94a3b8; }
    .icon {
      display: flex;
      align-items: center;
      flex: 0 0 auto;
      margin-left: 12px;
      color: #111111;
    }
    .icon svg { width: 18px; height: 18px; display: block; }
    .control input {
      flex: 1 1 auto;
      min-width: 0;
      width: 100%;
      height: 40px;
      margin: 0;
      padding: 0 12px 0 8px;
      border: 0;
      border-radius: 0;
      outline: none;
      box-shadow: none;
      background: transparent;
      appearance: none;
      -webkit-appearance: none;
      color: var(--ink);
      font: 14px/20px Inter, ui-sans-serif, system-ui, "Segoe UI", sans-serif;
    }
    .control input::placeholder { color: var(--faint); }
    .reveal {
      flex: 0 0 auto;
      height: 40px;
      border: 0;
      background: transparent;
      color: #111111;
      font: 12px/1 Inter, ui-sans-serif, system-ui, sans-serif;
      font-weight: 600;
      padding: 0 12px;
      cursor: pointer;
    }
    .note { color: var(--muted); font-size: 11px; line-height: 1.4; }
    button[type="submit"] {
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      width: 100%;
      margin-top: 4px;
      border: 1px solid #111111;
      border-radius: 4px;
      background: #111111;
      color: #fff;
      font: inherit;
      height: 40px;
      font-size: 12px;
      font-weight: 600;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      padding: 0 16px;
      cursor: pointer;
    }
    button[type="submit"]:hover { background: #000; }
    button[type="submit"]:disabled { cursor: wait; opacity: 0.75; }
    .done p { margin: 0; color: var(--body); }
  </style>
</head>
<body>
  <main>
    <section class="card form-card">
      <div class="heading">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path d="M12 3l7 3v6c0 4.2-2.8 7.4-7 8.8C7.8 19.4 5 16.2 5 12V6l7-3z" stroke="#111111" stroke-width="1.8"/>
          <path d="M9 12l2 2 4-4" stroke="#111111" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
        <h1>Sign in to Vault</h1>
        <span class="badge">auth/{{.AuthMount}}</span>
      </div>
      <dl class="meta">
        <div><dt>Vault endpoint</dt><dd>{{.Host}}</dd></div>
        <div><dt>Address</dt><dd>{{.Address}}</dd></div>
      </dl>
      {{if .Done}}
        <div class="done">
          <p class="lead">Signed in to {{.Host}}. You can close this tab and return to the terminal.</p>
        </div>
      {{else}}
        <p class="lead">Authenticate with your Vault username and password. The authenticator code is optional.</p>
        {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
        <form method="post" action="/">
          <input type="hidden" name="state" value="{{.State}}">
          <div class="field">
            <div class="field-label">
              <label for="username">Username</label>
              <span class="hint">Required</span>
            </div>
            <div class="control">
              <span class="icon" aria-hidden="true">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none"><path d="M8 8h8M8 12h8M8 16h5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>
              </span>
              <input id="username" name="username" value="{{.Username}}" autocomplete="username" placeholder="Username" required autofocus>
            </div>
          </div>
          <div class="field">
            <div class="field-label">
              <label for="password">Secret / password</label>
              <span class="hint">Required</span>
            </div>
            <div class="control">
              <span class="icon" aria-hidden="true">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none"><circle cx="8" cy="14" r="3.2" stroke="currentColor" stroke-width="1.8"/><path d="M11 14h9l-2 2 2 2" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>
              </span>
              <input id="password" name="password" type="password" autocomplete="current-password" placeholder="Password" required>
              <button class="reveal" id="toggle-password" type="button" aria-label="Show password">Show</button>
            </div>
          </div>
          <div class="field">
            <div class="field-label">
              <label for="otp">Authenticator token</label>
              <span class="hint optional">Optional</span>
            </div>
            <div class="control">
              <span class="icon" aria-hidden="true">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none"><rect x="7" y="3" width="10" height="18" rx="2" stroke="currentColor" stroke-width="1.8"/><path d="M11 18h2" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>
              </span>
              <input id="otp" name="otp" inputmode="numeric" autocomplete="one-time-code" spellcheck="false" placeholder="6-digit code">
            </div>
            <span class="note">Leave this blank unless your account asks for a code.</span>
          </div>
          <button type="submit">Sign in</button>
        </form>
      {{end}}
    </section>
  </main>
  <script>
    var toggle = document.getElementById("toggle-password");
    var password = document.getElementById("password");
    if (toggle && password) {
      toggle.addEventListener("click", function () {
        var hidden = password.type === "password";
        password.type = hidden ? "text" : "password";
        toggle.textContent = hidden ? "Hide" : "Show";
      });
    }
    var form = document.querySelector("form");
    if (form) {
      form.addEventListener("submit", function () {
        var button = form.querySelector("button[type=submit]");
        button.disabled = true;
        button.textContent = "Signing in";
      });
    }
  </script>
</body>
</html>
`))
