package internal

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileConfig represents the user configuration stored in TOML.
type FileConfig struct {
	APIKey           string
	RcloneRemote     string
	TemplateHubID    string
	TemplateCoverID  string
	TemplateReviewID string
}

// DefaultConfigPath returns ~/.tess/config.toml.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tess", "config.toml"), nil
}

// LoadConfig reads a minimal TOML and returns the FileConfig.
func LoadConfig(path string) (FileConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FileConfig{}, fmt.Errorf("config file not found: %s", path)
		}
		return FileConfig{}, err
	}
	defer f.Close()
	var cfg FileConfig
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
		return FileConfig{}, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return FileConfig{}, fmt.Errorf("missing 'api_key' in config: %s", path)
	}
	return cfg, nil
}

// EnsureConfigDir ensures the parent directory for path exists.
func EnsureConfigDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0o755)
}

// SaveConfig writes a minimal TOML to path.
func SaveConfig(path string, cfg FileConfig) error {
	if err := EnsureConfigDir(path); err != nil {
		return err
	}
	var b strings.Builder
	if strings.TrimSpace(cfg.APIKey) != "" {
		fmt.Fprintf(&b, "api_key = \"%s\"\n", escape(cfg.APIKey))
	}
	if strings.TrimSpace(cfg.RcloneRemote) != "" {
		fmt.Fprintf(&b, "rclone_remote = \"%s\"\n", escape(cfg.RcloneRemote))
	}
	if strings.TrimSpace(cfg.TemplateHubID) != "" {
		fmt.Fprintf(&b, "template_hub_id = \"%s\"\n", escape(cfg.TemplateHubID))
	}
	if strings.TrimSpace(cfg.TemplateCoverID) != "" {
		fmt.Fprintf(&b, "template_cover_id = \"%s\"\n", escape(cfg.TemplateCoverID))
	}
	if strings.TrimSpace(cfg.TemplateReviewID) != "" {
		fmt.Fprintf(&b, "template_review_id = \"%s\"\n", escape(cfg.TemplateReviewID))
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func escape(s string) string {
	// Very small escape to avoid stray quotes in TOML values we write.
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}
