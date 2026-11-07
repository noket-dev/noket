package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// The server normally reports both values; these apply only if it does not.
const (
	defaultLoginTimeout = 10 * time.Minute
	defaultPollInterval = 2 * time.Second
	pollRequestTimeout  = 15 * time.Second
)

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

// loginSession is the polling session the server opened for this login. Only
// the CLI holds the secret; the browser approves the id.
type loginSession struct {
	client       noketv1connect.AuthServiceClient
	id           string
	secret       string
	authorizeURL string
	interval     time.Duration
	expiresIn    time.Duration
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

	session, err := createLoginSession(cmd.Context(), apiBase)
	if err != nil {
		return err
	}

	// The URL comes from the server, which knows where its web app lives; it is
	// the only thing the browser needs, and the secret stays here.
	fmt.Fprintf(out, "Open this URL in your browser to log in:\n  %s\n\n", session.authorizeURL)
	if !openBrowser(session.authorizeURL) {
		fmt.Fprintln(out, "Could not open the browser automatically; copy the URL above.")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), session.expiresIn)
	defer cancel()

	progress := startProgress(cmd.ErrOrStderr(), "Logging in", "Waiting for the browser approval")
	defer func() { progress.Stop(err) }()

	if progress.enabled() {
		deadline := time.Now().Add(session.expiresIn)
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
		fmt.Fprintln(out, "Waiting for the browser to approve the login...")
	}

	token, err := session.wait(ctx)
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

// createLoginSession asks the API for a pollable session and the timings the
// CLI should follow while it waits for the browser.
func createLoginSession(ctx context.Context, apiBase string) (*loginSession, error) {
	client := noketv1connect.NewAuthServiceClient(&http.Client{Timeout: pollRequestTimeout}, apiBase)
	response, err := client.CreateCliAuthorization(ctx, connect.NewRequest(&noketv1.CreateCliAuthorizationRequest{}))
	if err != nil {
		return nil, fmt.Errorf("create login session: %w", err)
	}

	msg := response.Msg
	if msg.GetAuthorizationId() == "" || msg.GetAuthorizationSecret() == "" {
		return nil, errors.New("login response is missing the authorization id or secret")
	}
	if msg.GetAuthorizationUrl() == "" {
		return nil, errors.New("login response is missing the authorization URL")
	}

	session := &loginSession{
		client:       client,
		id:           msg.GetAuthorizationId(),
		secret:       msg.GetAuthorizationSecret(),
		authorizeURL: msg.GetAuthorizationUrl(),
		interval:     defaultPollInterval,
		expiresIn:    defaultLoginTimeout,
	}
	if msg.GetInterval() > 0 {
		session.interval = time.Duration(msg.GetInterval()) * time.Second
	}
	if msg.GetExpiresIn() > 0 {
		session.expiresIn = time.Duration(msg.GetExpiresIn()) * time.Second
	}
	return session, nil
}

// wait polls until the browser decides, the session expires, or ctx ends.
func (s *loginSession) wait(ctx context.Context) (string, error) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		status, token, err := s.poll(ctx)
		if err != nil {
			// The poll itself fails when ctx ends mid-request; report the wait
			// ending rather than a transport error.
			if ctx.Err() != nil {
				return "", loginWaitError(ctx, s.expiresIn)
			}
			return "", err
		}

		switch status {
		case noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_APPROVED:
			if token == "" {
				return "", errors.New("login response has no token")
			}
			return token, nil
		case noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_DENIED:
			return "", errors.New("login was denied in the browser")
		case noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_EXPIRED:
			return "", errors.New("the login session expired; run `noket login` again")
		case noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_PENDING:
			select {
			case <-ctx.Done():
				return "", loginWaitError(ctx, s.expiresIn)
			case <-ticker.C:
			}
		default:
			return "", fmt.Errorf("unexpected login status %v", status)
		}
	}
}

func (s *loginSession) poll(ctx context.Context) (noketv1.CliAuthorizationStatus, string, error) {
	response, err := s.client.PollCliAuthorization(ctx, connect.NewRequest(&noketv1.PollCliAuthorizationRequest{
		AuthorizationId:     s.id,
		AuthorizationSecret: s.secret,
	}))
	if err != nil {
		return noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_UNSPECIFIED, "", fmt.Errorf("poll login session: %w", err)
	}
	return response.Msg.GetStatus(), response.Msg.GetToken(), nil
}

// loginWaitError distinguishes the session timing out from the user pressing
// Ctrl-C, which also ends ctx.
func loginWaitError(ctx context.Context, timeout time.Duration) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("login timed out after %s", timeout)
	}
	return ctx.Err()
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
					return fmt.Errorf("noketd is running (pid %d); run `noket down first", pid)
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
