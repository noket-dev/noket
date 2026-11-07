package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type prompter struct {
	in  *bufio.Reader
	out io.Writer
}

func newPrompter(cmd *cobra.Command) *prompter {
	return &prompter{in: bufio.NewReader(cmd.InOrStdin()), out: cmd.OutOrStdout()}
}

// line asks for a value and returns def when the input is empty.
func (p *prompter) line(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s (%s): ", label, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", label)
	}
	text, err := p.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return def, nil
	}
	return text, nil
}

// secret reads a hidden value when stdin is a terminal. An empty answer means
// "not provided"; callers decide whether that keeps or clears the old key.
func (p *prompter) secret(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) && p.in.Buffered() == 0 {
		fmt.Fprintf(p.out, "%s: ", label)
		data, err := term.ReadPassword(fd)
		fmt.Fprintln(p.out)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	return p.line(label, "")
}

func (p *prompter) confirm(label string, defaultYes bool) (bool, error) {
	def := "y/N"
	if defaultYes {
		def = "Y/n"
	}
	answer, err := p.line(label+" ["+def+"]", "")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "":
		return defaultYes, nil
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, errors.New("enter y or n")
	}
}

// choose shows a numbered list and returns the selected index.
func (p *prompter) choose(label string, items []string) (int, error) {
	if len(items) == 0 {
		return -1, errors.New("no options available")
	}
	fmt.Fprintln(p.out, label)
	for i, item := range items {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, item)
	}
	for {
		answer, err := p.line("Enter a number", "")
		if err != nil {
			return -1, err
		}
		index, err := strconv.Atoi(answer)
		if err == nil && index >= 1 && index <= len(items) {
			return index - 1, nil
		}
		fmt.Fprintln(p.out, "Invalid number, try again.")
	}
}

// checkboxNumbered prints the list with its current selection and reads the
// numbers back. It is the path for piped input, where a form cannot run.
func (p *prompter) checkboxNumbered(label string, items []string, selected []bool) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(selected) != len(items) {
		selected = make([]bool, len(items))
	}
	fmt.Fprintln(p.out, label)
	for i, item := range items {
		mark := " "
		if selected[i] {
			mark = "x"
		}
		fmt.Fprintf(p.out, "  %d) [%s] %s\n", i+1, mark, item)
	}

	current := selectedItems(items, selected)
	def := joinIndexes(items, selected)
	answer, err := p.line("Enter numbers separated by commas (empty keeps the current selection)", def)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(answer) == "" {
		return current, nil
	}

	chosen := make(map[int]struct{})
	for _, part := range strings.Split(answer, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if index, err := strconv.Atoi(part); err == nil {
			if index < 1 || index > len(items) {
				return nil, fmt.Errorf("index %d out of range", index)
			}
			chosen[index-1] = struct{}{}
			continue
		}
		found := false
		for i, item := range items {
			if item == part {
				chosen[i] = struct{}{}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown model %q", part)
		}
	}

	out := make([]string, 0, len(chosen))
	for i, item := range items {
		if _, ok := chosen[i]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

func selectedItems(items []string, selected []bool) []string {
	out := make([]string, 0, len(items))
	for i, item := range items {
		if selected[i] {
			out = append(out, item)
		}
	}
	return out
}

func joinIndexes(items []string, selected []bool) string {
	parts := make([]string, 0, len(items))
	for i := range items {
		if selected[i] {
			parts = append(parts, strconv.Itoa(i+1))
		}
	}
	return strings.Join(parts, ",")
}
