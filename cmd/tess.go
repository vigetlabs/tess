package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	api "tess/internal"
)

func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tess", "config.toml"), nil
}

func loadAPIKeyFromTOML(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("config file not found: %s", path)
		}
		return "", err
	}
	defer f.Close()

	var apiKey string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Strip comments and whitespace
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") { // ignore sections
			continue
		}
		// Parse simple key = value pairs
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Trim surrounding quotes if present
		val = strings.Trim(val, " \t")
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key == "api_key" {
			apiKey = val
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(apiKey) == "" {
		return "", fmt.Errorf("missing 'api_key' in config: %s", path)
	}
	return apiKey, nil
}

func main() {
	cfgFlag := flag.String("config", "", "Path to config TOML (default: ~/.tess/config.toml)")
	flag.Parse()

	var cfgPath string
	if *cfgFlag != "" {
		cfgPath = *cfgFlag
	} else {
		var err error
		cfgPath, err = defaultConfigPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error determining default config path: %v\n", err)
			os.Exit(1)
		}
	}

	apiKey, err := loadAPIKeyFromTOML(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	client, err := api.NewClient(apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init api client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	me, err := client.GetMe(ctx)
	if err != nil {
		log.Fatalf("failed to fetch current user: %v", err)
	}

	reports, err := client.ListUsersByURL(ctx, me.DirectReports.URL)
	if err != nil {
		log.Fatalf("failed to fetch direct reports: %v", err)
	}

	sort.Slice(reports, func(i, j int) bool { return strings.ToLower(reports[i].Name) < strings.ToLower(reports[j].Name) })
	names := make([]string, 0, len(reports))
	for _, u := range reports {
		names = append(names, u.Name)
	}

	// Interactive selection: show list, print chosen name on Enter.
	m := newListModel(names)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		log.Fatalf("tui error: %v", err)
	}
	if m.choice == "" || len(reports) == 0 {
		return
	}

	// Find selected user's ID from cursor
	selIdx := m.cursor
	if selIdx < 0 || selIdx >= len(reports) {
		return
	}
	selectedUserID := reports[selIdx].ID

	cycles, err := client.ListReviewCycles(ctx)
	if err != nil {
		log.Fatalf("failed to fetch review cycles: %v", err)
	}
	for _, cy := range cycles {
		reviewees, err := client.ListRevieweesByURL(ctx, cy.Reviewees.URL)
		if err != nil {
			// Skip cycles we can't read
			continue
		}
		for _, rv := range reviewees {
			if rv.User.ID == selectedUserID {
				fmt.Println(cy.Name)
				break
			}
		}
	}
}

// --- Minimal Bubble Tea list model ---
type listModel struct {
	items  []string
	cursor int
	choice string
}

func newListModel(items []string) *listModel {
	return &listModel{items: items}
}

func (m *listModel) Init() tea.Cmd { return nil }

func (m *listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.items) > 0 {
				m.choice = m.items[m.cursor]
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *listModel) View() string {
	var b strings.Builder
	b.WriteString("Select a user (↑/↓, Enter, q):\n\n")
	for i, it := range m.items {
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		fmt.Fprintf(&b, "%s %s\n", cursor, it)
	}
	return b.String()
}
