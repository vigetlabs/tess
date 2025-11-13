package internal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

// RunSetup is an interactive first-time configuration helper.
// It prompts for the API key and optional rclone remote, then writes ~/.tess/config.toml.
func RunSetup(ctx context.Context) error {
	cfgPath, err := DefaultConfigPath()
	if err != nil {
		return fmt.Errorf("determine default config path: %w", err)
	}
	fmt.Printf("Tess setup\n\n")
	fmt.Printf("Config file: %s\n", cfgPath)
	// If a config already exists, offer to keep or overwrite minimal fields.
	existing := FileConfig{}
	hadExisting := false
	if _, err := os.Stat(cfgPath); err == nil {
		if c, err := LoadConfig(cfgPath); err == nil {
			existing = c
			hadExisting = true
		}
	}

	in := bufio.NewReader(os.Stdin)
	// API key
	apiKey := existing.APIKey
	if strings.TrimSpace(apiKey) != "" {
		fmt.Printf("Existing API key detected. Press Enter to keep, or paste a new key.\n")
	} else {
		fmt.Printf("Enter your Lattice API key (paste, then Enter).\n")
	}
	fmt.Printf("API key: ")
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line != "" {
		apiKey = line
	}
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("no API key provided")
	}

	// rclone remote (optional; default "drive")
	rremote := existing.RcloneRemote
	if strings.TrimSpace(rremote) == "" {
		rremote = "drive"
	}
	fmt.Printf("\nGoogle Drive (optional): rclone remote name [default: %s]\n", rremote)
	fmt.Printf("Remote name: ")
	rline, _ := in.ReadString('\n')
	rline = strings.TrimSpace(rline)
	if rline != "" {
		rremote = rline
	}

	// Save
	cfg := FileConfig{APIKey: apiKey, RcloneRemote: strings.TrimSpace(rremote)}
	if hadExisting {
		// Keep any template IDs that were already present.
		cfg.TemplateHubID = existing.TemplateHubID
		cfg.TemplateCoverID = existing.TemplateCoverID
		cfg.TemplateReviewID = existing.TemplateReviewID
	}
	if err := SaveConfig(cfgPath, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("\n✓ Wrote config to %s\n", cfgPath)
	// Quick dependency hints
	fmt.Printf("\nNext steps:\n")
	if err := RcloneAvailable(); err != nil {
		fmt.Printf("- Optional Drive upload: install rclone (https://rclone.org) and run 'rclone config' to add remote '%s'\n", rremote)
	} else {
		// If rclone is present, check whether the desired remote exists.
		exists, _ := RemoteExists(ctx, rremote)
		if !exists {
			fmt.Printf("- rclone remote '%s' not found. Create it now via rclone (will open a browser to authorize)? [Y/n]: ", rremote)
			ans, _ := in.ReadString('\n')
			ans = strings.ToLower(strings.TrimSpace(ans))
			if ans == "" || ans == "y" || ans == "yes" {
				fmt.Println()
				// Try non-interactive creation; if it fails, fall back to full wizard.
				if err := CreateDriveRemote(ctx, rremote, "drive"); err != nil {
					fmt.Printf("Automatic creation failed (%v). Launching rclone wizard...\n", err)
					if err := RunRcloneConfig(ctx); err != nil {
						fmt.Printf("(rclone config exited with error: %v)\n", err)
					}
				}
			} else {
				fmt.Printf("- You can create it anytime via: rclone config (choose Storage: drive)\n")
			}
		} else {
			fmt.Printf("- rclone remote '%s' found\n", rremote)
		}
	}
	if err := HasPandoc(); err != nil {
		fmt.Printf("- Optional: install pandoc (https://pandoc.org) for DOCX/PDF export\n")
	}
	fmt.Printf("- Run 'tess' to generate a report, or 'tess doctor' to verify your setup\n")
	return nil
}
