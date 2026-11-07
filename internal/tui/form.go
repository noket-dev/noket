package tui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/types"
	"github.com/noket-dev/noket/internal/upstream"
)

// fieldsForm asks for the parts of an upstream that are not the model list.
func (m *Model) fieldsForm() *huh.Form {
	description := "Leave empty if this upstream needs no API key."
	if m.modal.editing {
		description = "Leave empty to keep the current key while the URL is unchanged; empty on a new URL means no key."
	}

	fields := []huh.Field{
		huh.NewInput().
			Title("Upstream name").
			Value(&m.modal.name).
			Validate(func(value string) error {
				return config.ValidateName(strings.TrimSpace(value))
			}),
		huh.NewSelect[string]().
			Title("Protocol").
			Options(huh.NewOptions(types.ProtocolOpenAI, types.ProtocolAnthropic)...).
			Value(&m.modal.protocol),
		huh.NewInput().
			Title("Base URL").
			Value(&m.modal.baseURL).
			Validate(func(value string) error {
				_, err := netguard.ParseBaseURL(value)
				return err
			}),
		huh.NewInput().
			Title("API Key").
			Description(description).
			EchoMode(huh.EchoModePassword).
			Value(&m.modal.key),
	}
	if m.modal.editing && m.modal.expected != nil && m.modal.expected.HasAPIKey() {
		fields = append(fields, huh.NewConfirm().
			Title("Remove the stored API key?").
			Affirmative("Remove").
			Negative("Keep").
			Value(&m.modal.clearKey))
	}

	return m.finishForm(huh.NewForm(huh.NewGroup(fields...)))
}

func (m *Model) modelsForm() *huh.Form {
	options := make([]huh.Option[string], 0, len(m.modal.modelItems))
	for _, model := range m.modal.modelItems {
		options = append(options, huh.NewOption(model, model))
	}

	// huh counts the title in the height, and the viewport takes what is left.
	// Only long lists scroll; a short one gets a box that fits it. The keys that
	// select live on the help line under the popup.
	const maxModels = 12
	height := min(len(options), maxModels) + 1
	if m.height > 0 {
		// The popup adds a border, vertical padding and a title line; keep
		// those inside the body.
		height = min(height, max(4, m.bodyHeight()-7))
	}

	return m.finishForm(huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Models to expose").
			Options(options...).
			Height(height).
			Filterable(true).
			Value(&m.modal.modelSelected).
			Validate(func(selected []string) error {
				if len(selected) == 0 {
					return errors.New("select at least one model")
				}
				return nil
			}),
	)))
}

func (m *Model) manualForm() *huh.Form {
	return m.finishForm(huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Model ids").
			Description("Comma separated.").
			Value(&m.modal.manual).
			Validate(func(value string) error {
				if len(upstream.ParseList(value)) == 0 {
					return errors.New("enter at least one model id")
				}
				return nil
			}),
	)))
}

func (m *Model) recoveryForm() *huh.Form {
	description := ""
	if m.modal.probeErr != nil {
		description = m.modal.probeErr.Error()
	}
	return m.finishForm(huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Could not read the model list").
			Description(description).
			Options(
				huh.NewOption("Retry the request", "retry"),
				huh.NewOption("Change the base URL", "url"),
				huh.NewOption("Enter model ids manually", "manual"),
				huh.NewOption("Cancel", "cancel"),
			).
			Value(&m.modal.recovery),
	)))
}

func (m *Model) confirmRemoveForm(up types.Upstream) *huh.Form {
	return m.finishForm(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Remove upstream \"" + up.Name + "\"?").
			Description("This drops its " + strconv.Itoa(len(up.Models)) + " models from the local config.").
			Affirmative("Remove").
			Negative("Keep").
			Value(&m.modal.confirm),
	)))
}

func (m *Model) confirmStopForm(pid int) *huh.Form {
	return m.finishForm(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Stop noketd (pid " + strconv.Itoa(pid) + ")?").
			Description("The tunnel disconnects until you start it again.").
			Affirmative("Stop").
			Negative("Keep").
			Value(&m.modal.confirm),
	)))
}

func (m *Model) finishForm(form *huh.Form) *huh.Form {
	// The TUI draws its own keys line under the popup.
	return form.WithShowHelp(false).WithWidth(formWidth(m.width))
}

// modalWidth is the popup's outer width, border included.
func modalWidth(term int) int {
	if term <= 0 {
		return 60
	}
	return min(min(max(term*2/3, 44), 72), max(term, 20))
}

// formWidth is what a huh form may use inside the popup: the outer width minus
// the border and the two two-column paddings.
func formWidth(term int) int {
	return max(modalWidth(term)-6, 16)
}
