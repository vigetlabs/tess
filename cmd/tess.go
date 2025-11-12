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
	"unicode"

	bubspinner "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	api "tess/internal"
)

type fileConfig struct {
	APIKey           string
	RcloneRemote     string
	TemplateHubID    string
	TemplateCoverID  string
	TemplateReviewID string
}

func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tess", "config.toml"), nil
}

func loadConfigFromTOML(path string) (fileConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileConfig{}, fmt.Errorf("config file not found: %s", path)
		}
		return fileConfig{}, err
	}
	defer f.Close()
	var cfg fileConfig
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, " \t")
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		switch key {
		case "api_key":
			cfg.APIKey = val
		case "rclone_remote":
			cfg.RcloneRemote = strings.TrimSpace(val)
		case "template_hub_id":
			cfg.TemplateHubID = strings.TrimSpace(val)
		case "template_cover_id":
			cfg.TemplateCoverID = strings.TrimSpace(val)
		case "template_review_id":
			cfg.TemplateReviewID = strings.TrimSpace(val)
		}
	}
	if err := scanner.Err(); err != nil {
		return fileConfig{}, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fileConfig{}, fmt.Errorf("missing 'api_key' in config: %s", path)
	}
	return cfg, nil
}

func main() {
	cfgFlag := flag.String("config", "", "Path to config TOML (default: ~/.tess/config.toml)")
	rcloneRemote := flag.String("rclone-remote", "drive", "rclone remote name to upload to (default: drive)")
	rcloneFolderID := flag.String("rclone-folder-id", "", "Google Drive folder ID; if set, upload via rclone to this folder")
	uploadFormat := flag.String("upload-format", "docx", "Upload format when using rclone: docx (Google Doc import) or pdf")
	pdfEngine := flag.String("pdf-engine", "", "Preferred PDF engine for pandoc (e.g., tectonic, xelatex). Leave empty for auto.")
	copyTemplates := flag.Bool("copy-templates", false, "Copy template docs into the Drive folder after export")
	censorFlag := flag.Bool("censor", false, "Censor reviewer names, scores, and quotes in the output")
	templateHubID := flag.String("template-hub-id", "1HU2Jm_JLaLOLPR6V6HjPI4VzwzZRw_OCOvsT3rC_8G0", "Google Doc file ID for the Hub template")
	templateCoverID := flag.String("template-cover-id", "1vX9gElaEXkQYReZTEb1151x1JnYDSw64eObiWjS7Sp4", "Google Doc file ID for the Cover template")
	templateReviewID := flag.String("template-review-id", "1OLd7jgwsoKSFiTsiWtOjw9k_c9BfNhx0XRFdMYDaLP0", "Google Doc file ID for the Review template")
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

	cfg, err := loadConfigFromTOML(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	apiKey := cfg.APIKey

	client, err := api.NewClient(apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init api client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	meAny, err := runWithSpinner(ctx, "Loading current user...", func(c context.Context) (any, error) { return client.GetMe(c) })
	if err != nil {
		log.Fatalf("failed to fetch current user: %v", err)
	}
	me := meAny.(*api.User)

	reportsAny, err := runWithSpinner(ctx, "Loading direct reports...", func(c context.Context) (any, error) { return client.ListUsersByURL(c, me.DirectReports.URL) })
	if err != nil {
		log.Fatalf("failed to fetch direct reports: %v", err)
	}
	reports := reportsAny.([]api.User)

	sort.Slice(reports, func(i, j int) bool { return strings.ToLower(reports[i].Name) < strings.ToLower(reports[j].Name) })
	names := make([]string, 0, len(reports))
	for _, u := range reports {
		names = append(names, u.Name)
	}
	m := newListModel("Select a user", names)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		log.Fatalf("tui error: %v", err)
	}
	if m.choice == "" || len(reports) == 0 {
		return
	}
	selIdx := m.cursor
	if selIdx < 0 || selIdx >= len(reports) {
		return
	}
	selectedUserID := reports[selIdx].ID

	fmt.Fprintln(os.Stderr)
	cyclesAny, err := runWithSpinner(ctx, "Loading review cycles...", func(c context.Context) (any, error) { return client.ListReviewCycles(c) })
	if err != nil {
		log.Fatalf("failed to fetch review cycles: %v", err)
	}
	cycles := cyclesAny.([]api.ReviewCycle)

	type cycleEntry struct {
		Name, ReviewsURL string
		Cycle            api.ReviewCycle
	}
	// Show a spinner while filtering cycles down to those that include the selected user
	filteredAny, err := runWithSpinner(ctx, fmt.Sprintf("Filtering cycles for %s...", reports[selIdx].Name), func(c context.Context) (any, error) {
		out := make([]cycleEntry, 0)
		for _, cy := range cycles {
			reviewees, err := client.ListRevieweesByURL(c, cy.Reviewees.URL)
			if err != nil {
				continue
			}
			for _, rv := range reviewees {
				if rv.User.ID == selectedUserID {
					out = append(out, cycleEntry{Name: cy.Name, ReviewsURL: rv.Reviews.URL, Cycle: cy})
					break
				}
			}
		}
		return out, nil
	})
	if err != nil {
		log.Fatalf("failed to filter review cycles: %v", err)
	}
	filtered := filteredAny.([]cycleEntry)
	if len(filtered) == 0 {
		fmt.Fprintln(os.Stderr, "no cycles found for selected user")
		return
	}
	sort.Slice(filtered, func(i, j int) bool { return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name) })

	cycleNames := make([]string, len(filtered))
	for i, ce := range filtered {
		cycleNames[i] = ce.Name
	}
	m2 := newListModel("Select a cycle", cycleNames)
	if _, err := tea.NewProgram(m2).Run(); err != nil {
		log.Fatalf("tui error: %v", err)
	}
	if m2.choice == "" {
		return
	}
	idx := m2.cursor
	if idx < 0 || idx >= len(filtered) {
		return
	}

	fmt.Fprintln(os.Stderr)
	reviewsAny, err := runWithSpinner(ctx, "Fetching reviews for cycle: "+filtered[idx].Name+"...", func(c context.Context) (any, error) { return client.ListReviewsByURL(c, filtered[idx].ReviewsURL, 100) })
	if err != nil {
		log.Fatalf("failed to fetch reviews: %v", err)
	}
	reviews := reviewsAny.([]api.Review)

	selectedUserName := reports[selIdx].Name
	mdAny, err := runWithSpinner(ctx, "Generating markdown...", func(c context.Context) (any, error) {
		return buildMarkdown(c, client, selectedUserName, filtered[idx].Name, reviews, *censorFlag)
	})
	if err != nil {
		log.Fatalf("build markdown failed: %v", err)
	}
	md := mdAny.(string)
	fname := outputFileName(selectedUserName, filtered[idx].Name)
	if err := os.WriteFile(fname, []byte(md), 0644); err != nil {
		log.Fatalf("failed to write file: %v", err)
	}
	uploadedURL := ""
	if strings.TrimSpace(*rcloneFolderID) != "" {
		if err := api.RcloneAvailable(); err != nil {
			log.Fatalf("%v; install from https://rclone.org", err)
		}
		// Normalize format
		fmtStr := strings.ToLower(strings.TrimSpace(*uploadFormat))
		if fmtStr != "pdf" && fmtStr != "docx" {
			fmtStr = "docx"
		}
		if err := api.HasPandoc(); err != nil {
			fmt.Fprintln(os.Stderr, "pandoc not found; skipping Drive upload via rclone. Install pandoc to enable document export.")
		} else {
			docTitle := fmt.Sprintf("%s (%s)", selectedUserName, filtered[idx].Name)
			// Determine remote: CLI flag overrides config when explicitly provided
			remoteName := *rcloneRemote
			explicitRemoteFlag := false
			flag.Visit(func(f *flag.Flag) {
				if f.Name == "rclone-remote" {
					explicitRemoteFlag = true
				}
			})
			if !explicitRemoteFlag && strings.TrimSpace(cfg.RcloneRemote) != "" {
				remoteName = cfg.RcloneRemote
			}
			if fmtStr == "pdf" {
				pdfPath := filepath.Join(os.TempDir(), docTitle+".pdf")
				// Force a specific engine if provided; tectonic is preferred for LaTeX flow and sans font support.
				engine := strings.TrimSpace(*pdfEngine)
				_, err := runWithSpinner(ctx, "Converting to PDF...", func(c context.Context) (any, error) {
					return nil, api.ConvertMarkdownToPDFWithEngine(c, fname, pdfPath, engine)
				})
				if err != nil {
					log.Fatalf("pandoc conversion failed: %v", err)
				}
				// Upload as a regular PDF file (no import)
				uploadAny, err := runWithSpinner(ctx, "Uploading PDF via rclone...", func(c context.Context) (any, error) {
					return api.CopyToAndLink(c, remoteName, *rcloneFolderID, pdfPath, docTitle+".pdf", "")
				})
				if err != nil {
					log.Fatalf("rclone upload failed: %v", err)
				}
				if ln, ok := uploadAny.(string); ok && strings.TrimSpace(ln) != "" {
					uploadedURL = ln
				}
			} else {
				docxPath := filepath.Join(os.TempDir(), docTitle+".docx")
				_, err := runWithSpinner(ctx, "Converting to DOCX...", func(c context.Context) (any, error) { return nil, api.ConvertMarkdownToDOCX(c, fname, docxPath) })
				if err != nil {
					log.Fatalf("pandoc conversion failed: %v", err)
				}
				uploadAny, err := runWithSpinner(ctx, "Uploading via rclone...", func(c context.Context) (any, error) {
					return api.CopyToAndLink(c, remoteName, *rcloneFolderID, docxPath, docTitle, "docx")
				})
				if err != nil {
					log.Fatalf("rclone upload failed: %v", err)
				}
				if ln, ok := uploadAny.(string); ok && strings.TrimSpace(ln) != "" {
					uploadedURL = ln
				}
			}
		}
	}

	fmt.Println()
	fmt.Printf("Wrote %s\n", fname)
	if strings.TrimSpace(uploadedURL) != "" {
		fmt.Printf("Uploaded %s\n", uploadedURL)
	}

	// Optionally copy templates into the Drive folder
	if *copyTemplates {
		// Visual separation from upload summary
		fmt.Println()
		if strings.TrimSpace(*rcloneFolderID) == "" {
			fmt.Fprintln(os.Stderr, "--copy-templates requires --rclone-folder-id to be set")
		} else if err := api.RcloneAvailable(); err != nil {
			fmt.Fprintln(os.Stderr, "rclone not found; cannot copy templates")
		} else {
			remoteName := *rcloneRemote
			explicitRemoteFlag := false
			flag.Visit(func(f *flag.Flag) {
				if f.Name == "rclone-remote" {
					explicitRemoteFlag = true
				}
			})
			if !explicitRemoteFlag && strings.TrimSpace(cfg.RcloneRemote) != "" {
				remoteName = cfg.RcloneRemote
			}

			// Resolve template IDs: CLI overrides config if provided
			th := strings.TrimSpace(*templateHubID)
			tc := strings.TrimSpace(*templateCoverID)
			tr := strings.TrimSpace(*templateReviewID)
			if !flagIsSet("template-hub-id") && strings.TrimSpace(cfg.TemplateHubID) != "" {
				th = cfg.TemplateHubID
			}
			if !flagIsSet("template-cover-id") && strings.TrimSpace(cfg.TemplateCoverID) != "" {
				tc = cfg.TemplateCoverID
			}
			if !flagIsSet("template-review-id") && strings.TrimSpace(cfg.TemplateReviewID) != "" {
				tr = cfg.TemplateReviewID
			}

			copies := []struct{ id, name string }{
				{th, "Hub"}, {tc, "Cover"}, {tr, "Review"},
			}
			for _, cp := range copies {
				if cp.id == "" {
					continue
				}
				title := fmt.Sprintf("Copying template: %s...", cp.name)
				_, err := runWithSpinner(ctx, title, func(c context.Context) (any, error) {
					return nil, api.CopyByIDToFolder(c, remoteName, *rcloneFolderID, cp.id)
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to copy template %s: %v\n", cp.name, err)
					continue
				}
				// We keep the original name; link retrieval is skipped since name is unchanged.
			}
		}
	}
}

// flagIsSet reports whether a flag with the given name was explicitly provided.
func flagIsSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

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
	fmt.Fprintf(&b, "\n%s (↑/↓, Enter, q):\n\n", m.title)
	for i, it := range m.items {
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		fmt.Fprintf(&b, "%s %s\n", cursor, it)
	}
	return b.String()
}

func buildMarkdown(ctx context.Context, c *api.Client, userName, cycleName string, reviews []api.Review, censor bool) (string, error) {
	mask := func(s string) string {
		if !censor {
			return s
		}
		var b strings.Builder
		for _, r := range s {
			if unicode.IsSpace(r) {
				b.WriteRune(r)
			} else {
				b.WriteRune('▒')
			}
		}
		return b.String()
	}
	peerByQ := make(map[string][]api.Review)
	selfByQ := make(map[string][]api.Review)
	qOrderPeer, qOrderSelf := make([]string, 0), make([]string, 0)
	seenPeer, seenSelf := make(map[string]bool), make(map[string]bool)
	for _, r := range reviews {
		qid := r.Question.ID
		switch strings.ToLower(r.ReviewType) {
		case "self":
			selfByQ[qid] = append(selfByQ[qid], r)
			if !seenSelf[qid] {
				qOrderSelf = append(qOrderSelf, qid)
				seenSelf[qid] = true
			}
		default:
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
				fmt.Fprintf(&b, "%s (score: %s):\n\n", mask(name), mask(score))
			} else {
				fmt.Fprintf(&b, "%s:\n\n", mask(name))
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
			for _, line := range strings.Split(mask(quote), "\n") {
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
			for _, line := range strings.Split(mask(quote), "\n") {
				fmt.Fprintf(&b, "> %s\n", line)
			}
			b.WriteString("\n")
		}
	}
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
	return fmt.Sprintf("%s_%s_%s.md", toSlug(first), toSlug(last), toSlug(cycleName))
}

func sanitizeText(s string) string {
	if s == "" {
		return s
	}
	s = html.UnescapeString(s)
	repls := []struct{ old, new string }{{"<br>", "\n"}, {"<br/>", "\n"}, {"<br />", "\n"}, {"</p>", "\n"}, {"<p>", ""}}
	for _, r := range repls {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
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
	raw := strings.Split(b.String(), "\n")
	compact := make([]string, 0, len(raw))
	prevBlank := false
	for _, line := range raw {
		l := strings.TrimRight(line, " 	")
		isBlank := strings.TrimSpace(l) == ""
		if isBlank && prevBlank {
			continue
		}
		compact = append(compact, l)
		prevBlank = isBlank
	}
	return strings.TrimSpace(strings.Join(compact, "\n"))
}

type doneMsg struct {
	result any
	err    error
}
type spinModel struct {
	sp     bubspinner.Model
	title  string
	work   func(context.Context) (any, error)
	ctx    context.Context
	result any
	err    error
}

func newSpinModel(ctx context.Context, title string, fn func(context.Context) (any, error)) *spinModel {
	s := bubspinner.New()
	s.Spinner = bubspinner.Pulse
	return &spinModel{sp: s, title: title, work: fn, ctx: ctx}
}
func (m *spinModel) Init() tea.Cmd {
	run := func() tea.Msg { res, err := m.work(m.ctx); return doneMsg{result: res, err: err} }
	return tea.Batch(m.sp.Tick, run)
}
func (m *spinModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch dm := msg.(type) {
	case doneMsg:
		m.result, m.err = dm.result, dm.err
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}
}
func (m *spinModel) View() string { return fmt.Sprintf("%s %s", m.sp.View(), m.title) }
func runWithSpinner(ctx context.Context, title string, fn func(context.Context) (any, error)) (any, error) {
	m := newSpinModel(ctx, title, fn)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return nil, err
	}
	// Persist a final line so history remains
	fmt.Fprintf(os.Stderr, "✓ %s\n", title)
	return m.result, m.err
}

// buildHTMLDocument wraps Markdown content in minimal HTML for Drive import.

// buildHTMLDocument wraps Markdown content in minimal HTML for Drive import.
func buildHTMLDocument(title, md string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body>\n", html.EscapeString(title))
	b.WriteString(markdownToBasicHTML(md))
	b.WriteString("\n</body></html>")
	return b.String()
}

// markdownToBasicHTML converts a subset of our Markdown to simple HTML suitable for Drive import.
func markdownToBasicHTML(md string) string {
	lines := strings.Split(md, "\n")
	var b strings.Builder
	para := func(s string) {
		if strings.TrimSpace(s) != "" {
			fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(s))
		}
	}
	var acc []string
	flush := func() {
		if len(acc) > 0 {
			para(strings.Join(acc, " "))
			acc = nil
		}
	}
	for _, ln := range lines {
		if strings.HasPrefix(ln, "# ") {
			flush()
			fmt.Fprintf(&b, "<h1>%s</h1>\n", html.EscapeString(strings.TrimSpace(ln[2:])))
			continue
		}
		if strings.HasPrefix(ln, "## ") {
			flush()
			fmt.Fprintf(&b, "<h2>%s</h2>\n", html.EscapeString(strings.TrimSpace(ln[3:])))
			continue
		}
		if strings.HasPrefix(ln, "### ") {
			flush()
			fmt.Fprintf(&b, "<h3>%s</h3>\n", html.EscapeString(strings.TrimSpace(ln[4:])))
			continue
		}
		if strings.HasPrefix(ln, "> ") {
			flush()
			fmt.Fprintf(&b, "<blockquote>%s</blockquote>\n", html.EscapeString(strings.TrimSpace(strings.TrimPrefix(ln, "> "))))
			continue
		}
		if strings.TrimSpace(ln) == "" {
			flush()
			continue
		}
		acc = append(acc, ln)
	}
	flush()
	return b.String()
}
