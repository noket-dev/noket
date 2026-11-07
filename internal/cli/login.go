package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	noketv1 "github.com/noket-dev/noket/gen/noket/v1"
	"github.com/noket-dev/noket/gen/noket/v1/noketv1connect"
	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/noketd"
	"github.com/noket-dev/noket/internal/types"
)

const loginTimeout = 2 * time.Minute

type callbackResult struct {
	code string
	err  error
}

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in through the browser and save local credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd)
		},
	}
}

func runLogin(cmd *cobra.Command) (err error) {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	reportExistingLogin(out)
	if pid, running := noketd.Running(); running {
		if _, managed := noketd.RunningPID(); managed {
			fmt.Fprintf(out, "noketd is running (pid %d); run `noket down && noket up` to use the new login.\n\n", pid)
		} else {
			fmt.Fprintf(out, "noketd is running in the foreground (pid %d); restart it through its supervisor to use the new login.\n\n", pid)
		}
	}

	apiBase := strings.TrimRight(cfg.API.BaseURL, "/")
	webBase := strings.TrimRight(cfg.Web.BaseURL, "/")

	verifier, err := randomURLToken(32)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state, err := randomHex(16)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start local callback server: %w", err)
	}
	defer func() { _ = listener.Close() }()

	redirectURI := "http://" + listener.Addr().String() + "/callback"
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.Handle("/callback", &callbackHandler{state: state, result: resultCh})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	authorize := webBase + "/cli/login?" + url.Values{
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	fmt.Fprintf(out, "Open this URL in your browser to log in:\n  %s\n\n", authorize)
	if !openBrowser(authorize) {
		fmt.Fprintln(out, "Could not open the browser automatically; copy the URL above.")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), loginTimeout)
	defer cancel()

	progress := startProgress(cmd.ErrOrStderr(), "Logging in",
		"Waiting for the browser callback", "Exchanging the authorization code")
	defer func() { progress.Stop(err) }()

	if progress.enabled() {
		deadline := time.Now().Add(loginTimeout)
		tick := make(chan struct{})
		defer close(tick)
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-tick:
					return
				case now := <-ticker.C:
					remaining := deadline.Sub(now).Round(time.Second)
					if remaining < 0 {
						remaining = 0
					}
					progress.Detail(fmt.Sprintf("%s remaining", remaining))
				}
			}
		}()
	} else {
		fmt.Fprintln(out, "Waiting for the login callback...")
	}

	var result callbackResult
	select {
	case result = <-resultCh:
	case <-ctx.Done():
		return fmt.Errorf("login timed out after %s", loginTimeout)
	}
	if result.err != nil {
		return result.err
	}
	progress.Advance()

	token, err := exchangeCode(ctx, apiBase, result.code, verifier, redirectURI)
	if err != nil {
		return err
	}

	creds := &types.Credentials{
		Token:   token,
		UserID:  config.Subject(token),
		SavedAt: time.Now().UTC(),
	}
	if err := config.SaveCredentials(creds); err != nil {
		return err
	}
	path, _ := config.Path("credentials.json")

	if creds.UserID != "" {
		fmt.Fprintf(out, "Logged in as %s.\n", creds.UserID)
	} else {
		fmt.Fprintln(out, "Login successful.")
	}
	fmt.Fprintf(out, "Credentials saved to %s (readable only by the current user).\n", path)
	return nil
}

// reportExistingLogin tells the user a session is already saved before running
// the browser flow again. Login never blocks on it: it is also how the CLI
// switches accounts, and the new credentials simply replace the old ones.
func reportExistingLogin(out io.Writer) {
	creds, err := config.LoadCredentials()
	switch {
	case errors.Is(err, config.ErrNoCredentials):
		return
	case err != nil:
		fmt.Fprintf(out, "Warning: existing credentials could not be read: %v\n", err)
		fmt.Fprintln(out, "Logging in will replace them.")
		fmt.Fprintln(out)
		return
	}

	subject := creds.UserID
	if subject == "" {
		subject = config.Subject(creds.Token)
	}
	saved := ""
	if !creds.SavedAt.IsZero() {
		saved = fmt.Sprintf(" (credentials saved %s)", creds.SavedAt.Local().Format("2006-01-02 15:04"))
	}
	if subject != "" {
		fmt.Fprintf(out, "Already logged in as %s%s.\n", subject, saved)
	} else {
		fmt.Fprintf(out, "Already logged in%s.\n", saved)
	}
	fmt.Fprintln(out, "Logging in again replaces the saved credentials.")
	fmt.Fprintln(out)
}

type callbackHandler struct {
	state  string
	result chan<- callbackResult
}

func (h *callbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	if query.Get("state") != h.state {
		// A stray or forged callback must not end the real login attempt.
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	if message := query.Get("error"); message != "" {
		deliver(h.result, callbackResult{err: fmt.Errorf("login rejected: %s", message)})
		writeCallbackPage(w, false)
		return
	}
	code := query.Get("code")
	if code == "" {
		deliver(h.result, callbackResult{err: errors.New("login callback is missing code")})
		writeCallbackPage(w, false)
		return
	}

	deliver(h.result, callbackResult{code: code})
	writeCallbackPage(w, true)
}

func deliver(ch chan<- callbackResult, result callbackResult) {
	select {
	case ch <- result:
	default:
	}
}

func writeCallbackPage(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	message := "Login complete. You can close this page and return to the terminal."
	status := http.StatusOK
	if !ok {
		message = "Login failed. Return to the terminal for details."
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "<!doctype html><meta charset=\"utf-8\"><title>noket</title>"+
		"<body style=\"font:16px system-ui;padding:3rem;text-align:center\">"+
		"<p>"+message+"</p></body>")
}

func exchangeCode(ctx context.Context, apiBase, code, verifier, redirectURI string) (string, error) {
	client := noketv1connect.NewAuthServiceClient(&http.Client{Timeout: 15 * time.Second}, apiBase)
	response, err := client.ExchangeCliAuthorizationCode(ctx, connect.NewRequest(&noketv1.ExchangeCliAuthorizationCodeRequest{
		Code:         code,
		CodeVerifier: verifier,
		RedirectUri:  redirectURI,
	}))
	if err != nil {
		return "", fmt.Errorf("exchange login credentials: %w", err)
	}

	token := response.Msg.GetToken()
	if token == "" {
		return "", errors.New("login response has no token")
	}
	return token, nil
}

func randomURLToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate PKCE verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate login state: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func openBrowser(target string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start() == nil
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the local login state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pid, running := noketd.Running(); running {
				if _, managed := noketd.RunningPID(); managed {
					return fmt.Errorf("noketd is running (pid %d); run noket down first", pid)
				}
				return fmt.Errorf("noketd is running in the foreground (pid %d); stop that process first", pid)
			}
			if err := config.DeleteCredentials(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Removed the local login state.")
			return nil
		},
	}
}
