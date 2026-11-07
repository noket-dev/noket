package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/types"
	"github.com/noket-dev/noket/internal/upstream"
)

// errFormAborted tells the callers that the user left a form with ctrl+c.
var errFormAborted = errors.New("form aborted")

// canUseForm reports whether both sides of the command are terminals. Piped
// input or redirected output keeps the numbered prompts instead.
func canUseForm(cmd *cobra.Command) bool {
	in, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return false
	}
	out, ok := cmd.OutOrStdout().(*os.File)
	return ok && term.IsTerminal(int(out.Fd()))
}

// runForm renders the groups on stderr and keeps stdout for command results.
func runForm(cmd *cobra.Command, groups ...*huh.Group) error {
	form := huh.NewForm(groups...).
		WithInput(cmd.InOrStdin()).
		WithOutput(cmd.ErrOrStderr())
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return errFormAborted
		}
		return err
	}
	return nil
}

func reportCanceled(cmd *cobra.Command) error {
	fmt.Fprintln(cmd.OutOrStdout(), "Canceled.")
	return nil
}

// runFormFlow runs flow when the command is interactive and reports whether it
// did. Aborting a form is a normal cancellation, not an error.
func runFormFlow(cmd *cobra.Command, flow func() error) (bool, error) {
	if !canUseForm(cmd) {
		return false, nil
	}
	if err := flow(); err != nil {
		if errors.Is(err, errFormAborted) {
			return true, reportCanceled(cmd)
		}
		return true, err
	}
	return true, nil
}

// runAddForm asks for a name first, because an existing name turns the flow
// into an edit and its values become the form defaults.
func runAddForm(cmd *cobra.Command, cfg *types.Config) error {
	name, err := askName(cmd, "")
	if err != nil {
		return err
	}
	existing, editing := upstream.Find(cfg, name)
	if editing {
		fmt.Fprintf(cmd.OutOrStdout(), "Upstream %q already exists; editing it.\n", existing.Name)
	}
	return askFieldsAndSave(cmd, cfg, existing, name, editing)
}

// runEditForm keeps the current name as the default, so the same field doubles
// as a rename.
func runEditForm(cmd *cobra.Command, cfg *types.Config, existing *types.Upstream) error {
	name, err := askName(cmd, existing.Name)
	if err != nil {
		return err
	}
	return askFieldsAndSave(cmd, cfg, existing, name, true)
}

// validateNameForForm trims first so surrounding spaces never fail the name,
// which askName trims again before using it.
func validateNameForForm(value string) error {
	return config.ValidateName(strings.TrimSpace(value))
}

func askName(cmd *cobra.Command, def string) (string, error) {
	name := def
	err := runForm(cmd, huh.NewGroup(
		huh.NewInput().
			Title("Upstream name").
			Value(&name).
			Validate(validateNameForForm),
	))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(name), nil
}

func askFieldsAndSave(cmd *cobra.Command, cfg *types.Config, existing *types.Upstream, name string, editing bool) error {
	protocol := types.ProtocolOpenAI
	base := "http://127.0.0.1:11434"
	if editing {
		protocol = existing.Protocol
		base = existing.BaseURL
	}
	key := ""

	var up types.Upstream
fields:
	for {
		if err := askFields(cmd, &protocol, &base, &key, editing); err != nil {
			return err
		}
		parsed, err := netguard.ParseBaseURL(base)
		if err != nil {
			return err
		}
		up, err = upstream.Build(existing, upstream.Draft{Name: name, Protocol: protocol, BaseURL: parsed.String(), Key: upstream.KeyPointer(existing, parsed.String(), key)})
		if err != nil {
			return err
		}

		found, manual, retryFields, err := probeOrRecover(cmd, cfg, &up)
		if err != nil {
			return err
		}
		if retryFields {
			continue fields
		}
		if manual != nil {
			up.Models = manual
			break
		}

		items, preselected := upstream.Choices(found, existing, editing)
		chosen, err := pickModels(cmd, items, preselected)
		if err != nil {
			return err
		}
		up.Models = chosen
		break
	}

	if err := upstream.Save(cfg, up, existing); err != nil {
		return err
	}
	printUpstreamSaved(cmd, up, existing != nil)
	return nil
}

// askFields edits its arguments in place and validates the URL inline, so a bad
// address no longer discards the rest of the answers.
func askFields(cmd *cobra.Command, protocol, base, key *string, editing bool) error {
	description := "Leave empty if this upstream needs no API key."
	if editing {
		description = "Leave empty to keep the current key while the URL is unchanged; empty on a new URL means no key."
	}
	return runForm(cmd, huh.NewGroup(
		huh.NewSelect[string]().
			Title("Protocol").
			Options(huh.NewOptions(types.ProtocolOpenAI, types.ProtocolAnthropic)...).
			Value(protocol),
		huh.NewInput().
			Title("Base URL").
			Value(base).
			Validate(func(value string) error {
				_, err := netguard.ParseBaseURL(value)
				return err
			}),
		huh.NewInput().
			Title("API Key").
			Description(description).
			EchoMode(huh.EchoModePassword).
			Value(key),
	))
}

type recoveryAction int

const (
	recoveryRetry recoveryAction = iota
	recoveryEditURL
	recoveryManual
)

// probeOrRecover probes the upstream, then asks what to do when the model list
// cannot be read instead of silently falling back to manual input.
func probeOrRecover(cmd *cobra.Command, cfg *types.Config, up *types.Upstream) (models, manual []string, retryFields bool, err error) {
	for {
		found, probeErr := probeWithProgress(cmd, cfg, up)
		if probeErr == nil {
			return found, nil, false, nil
		}
		action, err := askRecovery(cmd, probeErr)
		if err != nil {
			return nil, nil, false, err
		}
		switch action {
		case recoveryRetry:
			continue
		case recoveryEditURL:
			return nil, nil, true, nil
		default:
			list, err := askManualModels(cmd)
			if err != nil {
				return nil, nil, false, err
			}
			return nil, list, false, nil
		}
	}
}

func probeWithProgress(cmd *cobra.Command, cfg *types.Config, up *types.Upstream) ([]string, error) {
	progress := startProgress(cmd.ErrOrStderr(), "Reading models",
		fmt.Sprintf("Requesting the model list from %s", up.BaseURL))
	found, err := upstream.Probe(cmd.Context(), cfg, *up)
	progress.Stop(err)
	if err == nil && len(found) == 0 {
		return nil, errors.New("the upstream returned no models")
	}
	return found, err
}

func askRecovery(cmd *cobra.Command, probeErr error) (recoveryAction, error) {
	choice := "retry"
	err := runForm(cmd, huh.NewGroup(
		huh.NewSelect[string]().
			Title("Could not read the model list").
			Description(probeErr.Error()).
			Options(
				huh.NewOption("Retry the request", "retry"),
				huh.NewOption("Change the base URL", "url"),
				huh.NewOption("Enter model ids manually", "manual"),
			).
			Value(&choice),
	))
	if err != nil {
		return recoveryRetry, err
	}
	return recoveryActionFor(choice), nil
}

func recoveryActionFor(choice string) recoveryAction {
	switch choice {
	case "url":
		return recoveryEditURL
	case "manual":
		return recoveryManual
	default:
		return recoveryRetry
	}
}

func askManualModels(cmd *cobra.Command) ([]string, error) {
	raw := ""
	err := runForm(cmd, huh.NewGroup(
		huh.NewInput().
			Title("Model ids").
			Description("Comma separated.").
			Value(&raw).
			Validate(func(value string) error {
				if len(upstream.ParseList(value)) == 0 {
					return errors.New("enter at least one model id")
				}
				return nil
			}),
	))
	if err != nil {
		return nil, err
	}
	return upstream.ParseList(raw), nil
}

// pickUpstreamForm lists the configured upstreams as a scrollable, filterable
// select. It reports false when no form can run, so the caller can fall back to
// the numbered prompt.
func pickUpstreamForm(cmd *cobra.Command, names []string) (index int, handled bool, err error) {
	if !canUseForm(cmd) {
		return 0, false, nil
	}
	options := make([]huh.Option[int], 0, len(names))
	for i, name := range names {
		options = append(options, huh.NewOption(name, i))
	}
	// The label lives on the group: a filtering select draws its filter input
	// where the field title would be.
	field := huh.NewSelect[int]().
		Options(options...).
		Filtering(true).
		Value(&index)
	if err := runForm(cmd, huh.NewGroup(field).Title("Select an upstream")); err != nil {
		return 0, true, err
	}
	return index, true, nil
}

// confirmRemove asks with a form on a terminal. It reports handled = false
// when no form can run, so the caller keeps the y/N prompt that scripts use.
func confirmRemove(cmd *cobra.Command, name string, models int) (ok, handled bool, err error) {
	if !canUseForm(cmd) {
		return false, false, nil
	}
	yes := false
	err = runForm(cmd, huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Remove upstream %q?", name)).
			Description(fmt.Sprintf("This drops its %d models from the local config.", models)).
			Affirmative("Remove").
			Negative("Keep").
			Value(&yes),
	))
	if err != nil {
		return false, true, err
	}
	return yes, true, nil
}

func pickModels(cmd *cobra.Command, items, preselected []string) ([]string, error) {
	chosenSet := make(map[string]struct{}, len(preselected))
	for _, model := range preselected {
		chosenSet[model] = struct{}{}
	}
	options := make([]huh.Option[string], 0, len(items))
	for _, model := range items {
		option := huh.NewOption(model, model)
		if _, ok := chosenSet[model]; ok {
			option = option.Selected(true)
		}
		options = append(options, option)
	}

	var chosen []string
	err := runForm(cmd, huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Models to expose").
			Description("Space toggles, / filters, enter confirms.").
			Options(options...).
			Height(12).
			Filterable(true).
			Value(&chosen).
			Validate(func(selected []string) error {
				if len(selected) == 0 {
					return errors.New("select at least one model")
				}
				return nil
			}),
	))
	if err != nil {
		return nil, err
	}
	return chosen, nil
}
