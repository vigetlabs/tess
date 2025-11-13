package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RcloneAvailable returns an error if rclone is not available in PATH.
func RcloneAvailable() error {
	if _, err := exec.LookPath("rclone"); err != nil {
		return fmt.Errorf("rclone not found in PATH: %w", err)
	}
	return nil
}

// CopyToAndLink copies a local file to Drive using rclone and returns a shareable link.
// If importFormat is non-empty (e.g. "docx" or "html"), it is passed via
// --drive-import-formats to let Drive import the content as a native Google Doc.
func CopyToAndLink(ctx context.Context, remoteName, folderID, srcPath, destRemote string, importFormat string) (string, error) {
	if err := RcloneAvailable(); err != nil {
		return "", err
	}
	args := []string{"copyto", srcPath, fmt.Sprintf("%s:%s", remoteName, destRemote)}
	if strings.TrimSpace(folderID) != "" {
		args = append(args, "--drive-root-folder-id="+folderID)
	}
	if strings.TrimSpace(importFormat) != "" {
		args = append(args, "--drive-import-formats", importFormat)
	}
	cmd := exec.CommandContext(ctx, "rclone", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("rclone copyto failed: %v: %s", err, string(out))
	}
	// Attempt to fetch a link to the uploaded file
	linkArgs := []string{"link", fmt.Sprintf("%s:%s", remoteName, destRemote)}
	if strings.TrimSpace(folderID) != "" {
		linkArgs = append(linkArgs, "--drive-root-folder-id="+folderID)
	}
	if out, err := exec.CommandContext(ctx, "rclone", linkArgs...).CombinedOutput(); err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	return "", nil
}

// CopyByIDToFolder performs a server-side copy of a Drive file (by file ID) into the
// specified Drive folder, preserving the original name and type. It does not return a link.
func CopyByIDToFolder(ctx context.Context, remoteName, folderID, fileID string) error {
	if err := RcloneAvailable(); err != nil {
		return err
	}
	if strings.TrimSpace(folderID) == "" {
		return fmt.Errorf("folderID is empty")
	}
	// Use destination fs with embedded root_folder_id to copy into the specific folder.
	dstFs := fmt.Sprintf("%s,root_folder_id=%s:", remoteName, folderID)
	args := []string{"backend", "copyid", remoteName + ":", fileID, dstFs, "--drive-server-side-across-configs"}
	cmd := exec.CommandContext(ctx, "rclone", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rclone backend copyid failed: %v: %s", err, string(out))
	}
	return nil
}

// RemoteExists returns true if an rclone remote with the given name exists.
func RemoteExists(ctx context.Context, name string) (bool, error) {
	if err := RcloneAvailable(); err != nil {
		return false, err
	}
	cmd := exec.CommandContext(ctx, "rclone", "listremotes")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("rclone listremotes failed: %w", err)
	}
	target := strings.TrimSpace(name)
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(strings.TrimSuffix(ln, ":"))
		if ln == target && ln != "" {
			return true, nil
		}
	}
	return false, nil
}

// RunRcloneConfig launches the interactive rclone config wizard attached to the current stdio.
func RunRcloneConfig(ctx context.Context) error {
	if err := RcloneAvailable(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "rclone", "config")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CreateDriveRemote attempts to non-interactively create a Google Drive remote
// with the given name and scope using rclone's config create command.
// It may still open a browser window to complete OAuth, but avoids the menu wizard.
func CreateDriveRemote(ctx context.Context, name string, scope string) error {
	if err := RcloneAvailable(); err != nil {
		return err
	}
	s := strings.TrimSpace(scope)
	if s == "" {
		s = "drive"
	}
	args := []string{"config", "create", name, "drive", "scope=" + s}
	cmd := exec.CommandContext(ctx, "rclone", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone config create failed: %w", err)
	}
	return nil
}
