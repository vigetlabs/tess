package internal

import (
	"context"
	"fmt"
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
