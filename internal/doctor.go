package internal

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// RunDoctor inspects the user's environment and prints actionable diagnostics.
func RunDoctor(ctx context.Context) int {
	// Status helpers
	ok := func(msg string) { fmt.Printf("✓ %s\n", msg) }
	warn := func(msg string) { fmt.Printf("! %s\n", msg) }
	bad := func(msg string) { fmt.Printf("✗ %s\n", msg) }

	// Config
	cfgPath, err := DefaultConfigPath()
	if err != nil {
		bad(fmt.Sprintf("determine config path: %v", err))
		return 1
	}
	fmt.Printf("Tess doctor\n\n")
	fmt.Printf("Config path: %s\n", cfgPath)
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		bad(err.Error())
		fmt.Printf("Hint: run 'tess setup' to create a config.\n")
		return 1
	}
	masked := maskToken(cfg.APIKey)
	ok("Loaded config")
	fmt.Printf("- api_key: %s\n", masked)
	if strings.TrimSpace(cfg.RcloneRemote) != "" {
		fmt.Printf("- rclone_remote: %s\n", strings.TrimSpace(cfg.RcloneRemote))
	}

	// API token check (lightweight /v1/me)
	client, err := NewClient(cfg.APIKey)
	if err != nil {
		bad(fmt.Sprintf("invalid API key: %v", err))
		return 1
	}
	if me, err := client.GetMe(ctx); err == nil && me != nil && strings.TrimSpace(me.ID) != "" {
		ok("Lattice API reachable and token accepted")
		fmt.Printf("- Current user: %s (%s)\n", me.Name, me.Email)
	} else if err != nil {
		bad(fmt.Sprintf("Lattice API check failed: %v", err))
		fmt.Printf("- Ensure your key is valid; if missing 'Bearer', Tess adds it automatically.\n")
	}

	// Optional tools
	if err := RcloneAvailable(); err != nil {
		warn("rclone not found (Drive upload disabled). Install from https://rclone.org")
	} else {
		ok("rclone found")
		// Check the configured remote exists (if provided)
		if strings.TrimSpace(cfg.RcloneRemote) != "" {
			exists, err := RemoteExists(ctx, cfg.RcloneRemote)
			if err != nil {
				warn(fmt.Sprintf("could not verify rclone remotes: %v", err))
			} else if !exists {
				warn(fmt.Sprintf("rclone remote '%s' not found. Run 'rclone config' and create it (Storage: drive)", cfg.RcloneRemote))
			} else {
				ok(fmt.Sprintf("rclone remote '%s' present", cfg.RcloneRemote))
			}
		}
	}
	if err := HasPandoc(); err != nil {
		warn("pandoc not found (DOCX/PDF export disabled). Install from https://pandoc.org")
	} else {
		ok("pandoc found")
	}

	// PATH sanity (best-effort)
	path := os.Getenv("PATH")
	if !strings.Contains(path, "/usr/local/bin") && !strings.Contains(path, "/opt/homebrew/bin") {
		warn("/usr/local/bin or /opt/homebrew/bin not in PATH (Homebrew installs may not be visible)")
	}

	fmt.Printf("\nAll done. If something looks off, try 'tess setup' or check the README.\n")
	return 0
}

func maskToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "(empty)"
	}
	// strip common prefixes for masking logic, then reattach
	lower := strings.ToLower(v)
	prefix := ""
	switch {
	case strings.HasPrefix(lower, "bearer "):
		prefix = v[:7]
		v = v[7:]
	case strings.HasPrefix(lower, "basic "):
		prefix = v[:6]
		v = v[6:]
	case strings.HasPrefix(lower, "token "):
		prefix = v[:6]
		v = v[6:]
	case strings.HasPrefix(lower, "lattice "):
		prefix = v[:8]
		v = v[8:]
	}
	if len(v) <= 8 {
		return prefix + strings.Repeat("*", len(v))
	}
	return prefix + v[:4] + strings.Repeat("*", len(v)-8) + v[len(v)-4:]
}
