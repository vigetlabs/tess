package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
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

	// Interactive selection: choose a report.
	m := newListModel("Select a user", names)
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
	type cycleEntry struct {
		Name       string
		ReviewsURL string
		Cycle      api.ReviewCycle
	}
	filtered := make([]cycleEntry, 0)
	for _, cy := range cycles {
		reviewees, err := client.ListRevieweesByURL(ctx, cy.Reviewees.URL)
		if err != nil {
			continue
		}
		for _, rv := range reviewees {
			if rv.User.ID == selectedUserID {
				filtered = append(filtered, cycleEntry{Name: cy.Name, ReviewsURL: rv.Reviews.URL, Cycle: cy})
				break
			}
		}
	}
	if len(filtered) == 0 {
		fmt.Fprintln(os.Stderr, "no cycles found for selected user")
		return
	}
	sort.Slice(filtered, func(i, j int) bool { return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name) })

	// Second-stage selection: pick a cycle
	cycleNames := make([]string, len(filtered))
	for i, ce := range filtered {
		cycleNames[i] = ce.Name
	}
	m2 := newListModel("Select a cycle", cycleNames)
	p2 := tea.NewProgram(m2)
	if _, err := p2.Run(); err != nil {
		log.Fatalf("tui error: %v", err)
	}
	if m2.choice == "" {
		return
	}
	idx := m2.cursor
	if idx < 0 || idx >= len(filtered) {
		return
	}

	// Fetch reviews (limit=100) for this reviewee-in-cycle
	reviews, err := client.ListReviewsByURL(ctx, filtered[idx].ReviewsURL, 100)
	if err != nil {
		log.Fatalf("failed to fetch reviews: %v", err)
	}

	// Generate markdown file and exit
	selectedUserName := reports[selIdx].Name
	md, err := buildMarkdown(ctx, client, selectedUserName, filtered[idx].Name, reviews)
	if err != nil {
		log.Fatalf("build markdown failed: %v", err)
	}
	fname := outputFileName(selectedUserName, filtered[idx].Name)
	if err := os.WriteFile(fname, []byte(md), 0644); err != nil {
		log.Fatalf("failed to write file: %v", err)
	}
	fmt.Println(fname)
}

// --- Minimal Bubble Tea list model ---
type listModel struct {
	title  string
	items  []string
	cursor int
	choice string
}

func newListModel(title string, items []string) *listModel {
	return &listModel{title: title, items: items}
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
	if m.title == "" {
		m.title = "Select"
	}
	fmt.Fprintf(&b, "%s (↑/↓, Enter, q):\n\n", m.title)
	for i, it := range m.items {
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		fmt.Fprintf(&b, "%s %s\n", cursor, it)
	}
	return b.String()
}

// --- Markdown builder and helpers ---
func buildMarkdown(ctx context.Context, c *api.Client, userName, cycleName string, reviews []api.Review) (string, error) {
	// Group by review type and by question ID
	peerByQ := make(map[string][]api.Review)
	selfByQ := make(map[string][]api.Review)
	qOrderPeer := make([]string, 0)
	qOrderSelf := make([]string, 0)
	seenPeer := make(map[string]bool)
	seenSelf := make(map[string]bool)
	for _, r := range reviews {
		qid := r.Question.ID
		switch strings.ToLower(r.ReviewType) {
		case "self":
			// Include self reviews even if unanswered; we'll render (no comment).
			selfByQ[qid] = append(selfByQ[qid], r)
			if !seenSelf[qid] {
				qOrderSelf = append(qOrderSelf, qid)
				seenSelf[qid] = true
			}
		default:
			// For peers/others, include only if there's content.
			if r.Response == nil {
				continue
			}
			hasContent := (r.Response.Comment != nil && strings.TrimSpace(*r.Response.Comment) != "") || len(r.Response.Choices) > 0 || r.Response.RatingString != nil || r.Response.Rating != nil
			if !hasContent {
				continue
			}
			peerByQ[qid] = append(peerByQ[qid], r)
			if !seenPeer[qid] {
				qOrderPeer = append(qOrderPeer, qid)
				seenPeer[qid] = true
			}
		}
	}

	var b strings.Builder
	// Title line with reviewee name and cycle name
	fmt.Fprintf(&b, "# %s (%s)\n\n", userName, cycleName)
	b.WriteString("## Peer Feedback\n\n")
	for _, qid := range qOrderPeer {
		qtext := "Question"
		if q, err := c.GetQuestionByID(ctx, qid); err == nil {
			qtext = html.UnescapeString(strings.TrimSpace(q.Body))
			qtext = strings.ReplaceAll(qtext, "\n", " ")
		}
		fmt.Fprintf(&b, "### %s\n\n", qtext)
		for _, r := range peerByQ[qid] {
			name := "Unknown"
			if r.Reviewer.ID != "" {
				if u, err := c.GetUserByID(ctx, r.Reviewer.ID); err == nil && strings.TrimSpace(u.Name) != "" {
					name = u.Name
				}
			}
			var score string
			if r.Response.RatingString != nil && *r.Response.RatingString != "" {
				score = *r.Response.RatingString
			}
			if score == "" && r.Response.Rating != nil {
				score = fmt.Sprintf("%.2f", *r.Response.Rating)
			}
			if score != "" {
				fmt.Fprintf(&b, "%s (score: %s):\n\n", name, score)
			} else {
				fmt.Fprintf(&b, "%s:\n\n", name)
			}
			quote := ""
			if r.Response.Comment != nil && strings.TrimSpace(*r.Response.Comment) != "" {
				quote = sanitizeText(strings.TrimSpace(*r.Response.Comment))
			} else if len(r.Response.Choices) > 0 {
				quote = sanitizeText(strings.Join(r.Response.Choices, ", "))
			}
			if strings.TrimSpace(quote) == "" {
				quote = "(no comment)"
			}
			for _, line := range strings.Split(quote, "\n") {
				fmt.Fprintf(&b, "> %s\n", line)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("---\n\n")
	b.WriteString("## Self Review\n\n")
	for _, qid := range qOrderSelf {
		qtext := "Question"
		if q, err := c.GetQuestionByID(ctx, qid); err == nil {
			qtext = sanitizeText(strings.TrimSpace(q.Body))
			qtext = strings.ReplaceAll(qtext, "\n", " ")
		}
		fmt.Fprintf(&b, "### %s\n\n", qtext)
		for _, r := range selfByQ[qid] {
			quote := ""
			if r.Response != nil && r.Response.Comment != nil && strings.TrimSpace(*r.Response.Comment) != "" {
				quote = sanitizeText(strings.TrimSpace(*r.Response.Comment))
			} else if r.Response != nil && len(r.Response.Choices) > 0 {
				quote = sanitizeText(strings.Join(r.Response.Choices, ", "))
			}
			if strings.TrimSpace(quote) == "" {
				quote = "(no comment)"
			}
			for _, line := range strings.Split(quote, "\n") {
				fmt.Fprintf(&b, "> %s\n", line)
			}
			b.WriteString("\n")
		}
	}
	// Removed trailing divider
	return b.String(), nil
}

func outputFileName(userName, cycleName string) string {
	toSlug := func(s string) string {
		s = strings.ToLower(s)
		repl := func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			if r == ' ' || r == '-' || r == '/' || r == '\\' {
				return '_'
			}
			return -1
		}
		return strings.Trim(strings.Map(repl, s), "_")
	}
	first, last := "", ""
	parts := strings.Fields(userName)
	if len(parts) > 0 {
		first = parts[0]
	}
	if len(parts) > 1 {
		last = parts[len(parts)-1]
	}
	if first == "" {
		first = "user"
	}
	fname := fmt.Sprintf("%s_%s_%s.md", toSlug(first), toSlug(last), toSlug(cycleName))
	return fname
}

// sanitizeText converts HTML-ish input to readable plain text:
// - Unescapes entities (e.g., &#39; -> ')
// - Converts <br>, <br/>, <br /> to newlines; removes remaining tags
func sanitizeText(s string) string {
	if s == "" {
		return s
	}
	s = html.UnescapeString(s)
	// Normalize common break/paragraph tags to newlines
	repls := []struct{ old, new string }{
		{"<br>", "\n"}, {"<br/>", "\n"}, {"<br />", "\n"},
		{"</p>", "\n"}, {"<p>", ""},
	}
	for _, r := range repls {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
	// Strip any remaining tags
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			if inTag {
				inTag = false
			}
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	// Trim trailing spaces on lines
	outLines := make([]string, 0)
	for _, line := range strings.Split(b.String(), "\n") {
		outLines = append(outLines, strings.TrimRight(line, " \t"))
	}
	return strings.TrimSpace(strings.Join(outLines, "\n"))
}
