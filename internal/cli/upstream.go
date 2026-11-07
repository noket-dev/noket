package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/types"
	"github.com/noket-dev/noket/internal/upstream"
)

func newUpstreamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upstream",
		Short: "Manage local upstream LLM APIs",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		newUpstreamAddCmd(),
		newUpstreamEditCmd(),
		newUpstreamListCmd(),
		newUpstreamTestCmd(),
		newUpstreamRemoveCmd(),
	)
	return cmd
}

func newUpstreamAddCmd() *cobra.Command {
	var (
		name         string
		protocol     string
		baseURL      string
		keyFromStdin bool
		models       []string
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add an upstream; edit it when the name already exists",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			nonInteractive := cmd.Flags().Changed("name") || cmd.Flags().Changed("protocol") ||
				cmd.Flags().Changed("url") || cmd.Flags().Changed("model") || cmd.Flags().Changed("api-key-stdin")
			if nonInteractive {
				return runAddNonInteractive(cmd, name, protocol, baseURL, keyFromStdin, models)
			}
			return runAddInteractive(cmd)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Upstream name")
	cmd.Flags().StringVar(&protocol, "protocol", types.ProtocolOpenAI, "openai or anthropic")
	cmd.Flags().StringVar(&baseURL, "url", "", "upstream base URL (with or without /v1)")
	cmd.Flags().BoolVar(&keyFromStdin, "api-key-stdin", false, "read one API key line from stdin")
	cmd.Flags().StringArrayVar(&models, "model", nil, "model id to expose (repeatable)")
	return cmd
}

func newUpstreamEditCmd() *cobra.Command {
	var (
		newName      string
		protocol     string
		baseURL      string
		keyFromStdin bool
		clearKey     bool
		models       []string
	)
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit an upstream",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nonInteractive := cmd.Flags().Changed("new-name") || cmd.Flags().Changed("protocol") ||
				cmd.Flags().Changed("url") || cmd.Flags().Changed("model") ||
				cmd.Flags().Changed("api-key-stdin") || cmd.Flags().Changed("clear-api-key")
			if nonInteractive {
				return runEditNonInteractive(cmd, args[0], newName, protocol, baseURL, keyFromStdin, clearKey, models)
			}
			return runEditInteractive(cmd, args[0])
		},
	}
	cmd.Flags().StringVar(&newName, "new-name", "", "rename the upstream")
	cmd.Flags().StringVar(&protocol, "protocol", "", "openai or anthropic")
	cmd.Flags().StringVar(&baseURL, "url", "", "upstream base URL (with or without /v1)")
	cmd.Flags().BoolVar(&keyFromStdin, "api-key-stdin", false, "read one API key line from stdin")
	cmd.Flags().BoolVar(&clearKey, "clear-api-key", false, "clear the saved API key")
	cmd.Flags().StringArrayVar(&models, "model", nil, "complete model list (repeatable)")
	return cmd
}

func newUpstreamListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List upstreams and models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(cfg.Upstreams) == 0 {
				fmt.Fprintln(out, "No upstreams configured. Run noket upstream add.")
				return nil
			}

			printUpstreamTable(out, cfg.Upstreams)
			return nil
		},
	}
}

const (
	upstreamColName = iota
	upstreamColProtocol
	upstreamColURL
	upstreamColModels
	upstreamColKey
)

// printUpstreamTable renders the aligned table on a terminal and keeps the
// plain one for pipes, so scripts see the same bytes as before.
func printUpstreamTable(out io.Writer, upstreams []types.Upstream) {
	rows := [][]string{{"Name", "Protocol", "Base URL", "Models", "Key"}}
	withKey := make([]bool, 0, len(upstreams))
	for _, up := range upstreams {
		key := "not set"
		if up.HasAPIKey() {
			key = "set"
		}
		rows = append(rows, []string{up.Name, up.Protocol, up.BaseURL, strings.Join(up.Models, ", "), key})
		withKey = append(withKey, up.HasAPIKey())
	}

	if interactiveOutput(out) {
		printStyledUpstreamTable(out, rows, withKey)
		return
	}
	printPlainUpstreamTable(out, rows)
}

func printStyledUpstreamTable(out io.Writer, rows [][]string, withKey []bool) {
	renderer := table.New().
		Headers(rows[0]...).
		Rows(rows[1:]...).
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderHeader(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			// One column of left padding reproduces the plain table's gutter.
			style := lipgloss.NewStyle().PaddingLeft(1)
			if row == table.HeaderRow {
				return style.Inherit(styleBold)
			}
			if col == upstreamColKey {
				if withKey[row] {
					return style.Inherit(styleOK)
				}
				return style.Inherit(styleFaint)
			}
			return style
		})
	fmt.Fprintln(out, renderer.Render())
}

// printPlainUpstreamTable pads by display width so wide characters, such as
// ones in model ids, still line up.
func printPlainUpstreamTable(out io.Writer, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], cellWidth(cell))
		}
	}
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			line.WriteString(cell)
			if i == len(row)-1 {
				break
			}
			line.WriteString(strings.Repeat(" ", widths[i]-cellWidth(cell)+2))
		}
		fmt.Fprintln(out, strings.TrimRight(line.String(), " "))
	}
}

func cellWidth(cell string) int {
	width := 0
	for _, r := range cell {
		switch {
		case r >= 0x1100 && r <= 0x115F,
			r >= 0x2E80 && r <= 0xA4CF,
			r >= 0xAC00 && r <= 0xD7A3,
			r >= 0xF900 && r <= 0xFAFF,
			r >= 0xFE30 && r <= 0xFE4F,
			r >= 0xFF00 && r <= 0xFF60,
			r >= 0xFFE0 && r <= 0xFFE6:
			width += 2
		default:
			width++
		}
	}
	return width
}

func newUpstreamTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [name]",
		Short: "Request the upstream /v1/models endpoint to check the URL and key",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			up, err := pickUpstream(cmd, cfg, args)
			if err != nil {
				if errors.Is(err, errFormAborted) {
					return reportCanceled(cmd)
				}
				return err
			}

			progress := startProgress(cmd.ErrOrStderr(), fmt.Sprintf("Testing upstream %q", up.Name),
				fmt.Sprintf("Requesting the model list from %s", up.BaseURL))

			models, err := upstream.Probe(cmd.Context(), cfg, *up)
			progress.Stop(err)
			if err != nil {
				return fmt.Errorf("connect to upstream %q: %w", up.Name, err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Connection OK, found %d models:\n", len(models))
			for _, model := range models {
				fmt.Fprintf(out, "  %s\n", model)
			}
			return nil
		},
	}
}

func newUpstreamRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove [name]",
		Short: "Remove an upstream",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			var up *types.Upstream
			if len(args) == 1 {
				found, ok := upstream.Find(cfg, args[0])
				if !ok {
					return fmt.Errorf("upstream %q does not exist", args[0])
				}
				up = found
			} else {
				up, err = pickUpstream(cmd, cfg, nil)
				if err != nil {
					if errors.Is(err, errFormAborted) {
						return reportCanceled(cmd)
					}
					return err
				}
			}

			// Capture these before removing: up points into cfg.Upstreams and
			// the delete shifts the following entries.
			name := up.Name
			modelCount := len(up.Models)
			if !yes {
				ok, handled, err := confirmRemove(cmd, name, modelCount)
				if err != nil {
					if errors.Is(err, errFormAborted) {
						return reportCanceled(cmd)
					}
					return err
				}
				if !handled {
					ok, err = newPrompter(cmd).confirm(
						fmt.Sprintf("Remove upstream %q and its %d models?", name, modelCount), false)
					if err != nil {
						return err
					}
				}
				if !ok {
					fmt.Fprintln(cmd.OutOrStdout(), "Canceled.")
					return nil
				}
			}

			if !upstream.Remove(cfg, name) {
				return fmt.Errorf("upstream %q does not exist", name)
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			path, _ := config.ConfigPath()
			fmt.Fprintf(cmd.OutOrStdout(), "Removed upstream %q; config saved to %s.\n", name, path)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func runAddNonInteractive(cmd *cobra.Command, name, protocol, baseURL string, keyFromStdin bool, models []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("--name is required for non-interactive add")
	}
	if strings.TrimSpace(baseURL) == "" {
		return errors.New("--url is required for non-interactive add")
	}
	if len(models) == 0 {
		return errors.New("at least one --model is required for non-interactive add")
	}

	existing, editing := upstream.Find(cfg, name)
	effectiveProtocol := protocol
	if editing && !cmd.Flags().Changed("protocol") {
		effectiveProtocol = existing.Protocol
	}

	var key *string
	if keyFromStdin {
		value, err := readKeyFromStdin(cmd)
		if err != nil {
			return err
		}
		key = &value
	}
	up, err := upstream.Build(existing, upstream.Draft{Name: name, Protocol: effectiveProtocol, BaseURL: baseURL, Key: key, Models: models})
	if err != nil {
		return err
	}
	if err := upstream.Save(cfg, up, existing); err != nil {
		return err
	}
	printUpstreamSaved(cmd, up, editing)
	return nil
}

func runEditNonInteractive(cmd *cobra.Command, name, newName, protocol, baseURL string, keyFromStdin, clearKey bool, models []string) error {
	if keyFromStdin && clearKey {
		return errors.New("--api-key-stdin and --clear-api-key cannot be used together")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	existing, ok := upstream.Find(cfg, name)
	if !ok {
		return fmt.Errorf("upstream %q does not exist", name)
	}

	if !cmd.Flags().Changed("protocol") {
		protocol = existing.Protocol
	}
	if !cmd.Flags().Changed("url") {
		baseURL = existing.BaseURL
	}
	if !cmd.Flags().Changed("model") {
		models = existing.Models
	}
	if newName == "" {
		newName = existing.Name
	}

	var key *string
	switch {
	case keyFromStdin:
		value, err := readKeyFromStdin(cmd)
		if err != nil {
			return err
		}
		key = &value
	case clearKey:
		empty := ""
		key = &empty
	}

	up, err := upstream.Build(existing, upstream.Draft{Name: newName, Protocol: protocol, BaseURL: baseURL, Key: key, Models: models})
	if err != nil {
		return err
	}
	if err := upstream.Save(cfg, up, existing); err != nil {
		return err
	}
	printUpstreamSaved(cmd, up, true)
	return nil
}

func runAddInteractive(cmd *cobra.Command) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if handled, err := runFormFlow(cmd, func() error { return runAddForm(cmd, cfg) }); handled {
		return err
	}
	p := newPrompter(cmd)

	name, err := p.line("Upstream name", "")
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("upstream name is required")
	}
	existing, editing := upstream.Find(cfg, name)
	if editing {
		fmt.Fprintf(cmd.OutOrStdout(), "Upstream %q already exists; editing it.\n", existing.Name)
	}
	return runInteractiveFields(cmd, cfg, p, existing, name, editing)
}

func runEditInteractive(cmd *cobra.Command, name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	existing, ok := upstream.Find(cfg, name)
	if !ok {
		return fmt.Errorf("upstream %q does not exist", name)
	}
	if handled, err := runFormFlow(cmd, func() error { return runEditForm(cmd, cfg, existing) }); handled {
		return err
	}
	p := newPrompter(cmd)

	answer, err := p.line("Upstream name", existing.Name)
	if err != nil {
		return err
	}
	return runInteractiveFields(cmd, cfg, p, existing, answer, true)
}

func runInteractiveFields(cmd *cobra.Command, cfg *types.Config, p *prompter, existing *types.Upstream, name string, editing bool) error {
	protocol := types.ProtocolOpenAI
	baseDefault := "http://127.0.0.1:11434"
	if editing {
		protocol = existing.Protocol
		baseDefault = existing.BaseURL
	}

	protocol, err := p.line("Protocol [openai/anthropic]", protocol)
	if err != nil {
		return err
	}
	answer, err := p.line("Base URL", baseDefault)
	if err != nil {
		return err
	}
	base, err := netguard.ParseBaseURL(answer)
	if err != nil {
		return err
	}

	sameAddress := editing && existing.BaseURL == base.String()
	label := "API Key (empty means not needed)"
	switch {
	case editing && sameAddress:
		label = "API Key (empty keeps the existing key)"
	case editing:
		label = "API Key (empty means this URL needs no key)"
	}
	entered, err := p.secret(label)
	if err != nil {
		return err
	}

	var key *string
	switch {
	case entered != "":
		key = &entered
	case editing && sameAddress:
		value := existing.APIKey
		key = &value
	case editing:
		empty := ""
		key = &empty
	default:
		empty := ""
		key = &empty
	}

	up, err := upstream.Build(existing, upstream.Draft{Name: name, Protocol: protocol, BaseURL: base.String(), Key: key})
	if err != nil {
		return err
	}

	items, preselected := upstream.Existing(existing)
	found, probeErr := upstream.Probe(cmd.Context(), cfg, up)
	if probeErr != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Failed to read models: %v\n", probeErr)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Reading models... found %d models\n", len(found))
		items = upstream.Union(found, items)
		if !editing {
			preselected = items
		}
	}

	if len(items) == 0 {
		manual, err := p.line("Enter model ids, comma separated", "")
		if err != nil {
			return err
		}
		items = upstream.ParseList(manual)
		if len(items) == 0 {
			return errors.New("at least one model id is required")
		}
		preselected = items
	}

	selected := make([]bool, len(items))
	preselectedSet := make(map[string]struct{}, len(preselected))
	for _, model := range preselected {
		preselectedSet[model] = struct{}{}
	}
	for i, item := range items {
		_, selected[i] = preselectedSet[item]
	}

	chosen, err := p.checkboxNumbered("Select the models to expose", items, selected)
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		return errors.New("select at least one model")
	}
	up.Models = chosen

	if err := upstream.Save(cfg, up, existing); err != nil {
		return err
	}
	printUpstreamSaved(cmd, up, editing)
	return nil
}

func printUpstreamSaved(cmd *cobra.Command, up types.Upstream, editing bool) {
	out := cmd.OutOrStdout()
	path, _ := config.ConfigPath()
	action := "Added"
	if editing {
		action = "Updated"
	}
	fmt.Fprintf(out, "%s upstream %q; config saved to %s.\n", action, up.Name, path)
	fmt.Fprintln(out, "\nNext steps:")
	fmt.Fprintln(out, "  noket up")
	fmt.Fprintln(out, "  Then create a model on the web and select this node, protocol, and model id")
}

func pickUpstream(cmd *cobra.Command, cfg *types.Config, args []string) (*types.Upstream, error) {
	if len(args) == 1 {
		up, ok := upstream.Find(cfg, args[0])
		if !ok {
			return nil, fmt.Errorf("upstream %q does not exist", args[0])
		}
		return up, nil
	}
	if len(cfg.Upstreams) == 0 {
		return nil, errors.New("no upstreams configured")
	}
	if len(cfg.Upstreams) == 1 {
		return &cfg.Upstreams[0], nil
	}
	names := make([]string, len(cfg.Upstreams))
	for i := range cfg.Upstreams {
		names[i] = cfg.Upstreams[i].Name
	}
	index, handled, err := pickUpstreamForm(cmd, names)
	if err != nil {
		return nil, err
	}
	if handled {
		return &cfg.Upstreams[index], nil
	}
	index, err = newPrompter(cmd).choose("Select an upstream", names)
	if err != nil {
		return nil, err
	}
	return &cfg.Upstreams[index], nil
}

func readKeyFromStdin(cmd *cobra.Command) (string, error) {
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
