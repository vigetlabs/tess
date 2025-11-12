package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// UploadFileToDrive uploads a local file using rclone instead of the Google Drive API.
// It assumes an rclone remote is configured (default remote name is "drive" or
// can be overridden via env var TESS_RCLONE_REMOTE). If folderID is provided,
// it is passed as --drive-root-folder-id to ensure the correct target folder.
// Returns a shareable link if rclone link succeeds, otherwise an empty string.
func UploadFileToDrive(ctx context.Context, _ string, folderID, localPath, mimeType string) (string, error) {
	remote := os.Getenv("TESS_RCLONE_REMOTE")
	if strings.TrimSpace(remote) == "" {
		remote = "drive"
	}
	dest := os.Getenv("TESS_RCLONE_DEST") // optional path under remote root

	if _, err := exec.LookPath("rclone"); err != nil {
		return "", fmt.Errorf("rclone not found in PATH: %w", err)
	}

	args := []string{"copy", localPath, fmt.Sprintf("%s:%s", remote, strings.TrimPrefix(dest, "/"))}
	if strings.TrimSpace(folderID) != "" {
		args = append(args, "--drive-root-folder-id="+folderID)
	}
	// When uploading HTML for Drive import -> Google Doc, caller may set mimeType
	// to text/html and set env TESS_RCLONE_IMPORT_HTML=1 to force import.
	if strings.EqualFold(mimeType, "text/html") || os.Getenv("TESS_RCLONE_IMPORT_HTML") == "1" {
		args = append(args, "--drive-import-formats", "html")
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("rclone copy failed: %v: %s", err, string(out))
	}

	// Attempt to get a shareable link for the uploaded file
	base := filepath.Base(localPath)
	linkArgs := []string{"link", fmt.Sprintf("%s:%s", remote, strings.TrimPrefix(filepath.Join(dest, base), "/"))}
	if strings.TrimSpace(folderID) != "" {
		linkArgs = append(linkArgs, "--drive-root-folder-id="+folderID)
	}
	if out, err := exec.CommandContext(ctx, "rclone", linkArgs...).CombinedOutput(); err == nil {
		ln := strings.TrimSpace(string(out))
		return ln, nil
	}
	return "", nil
}
