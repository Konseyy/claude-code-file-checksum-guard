package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const checksumDir = "/tmp/claude-file-checksums"

type ToolInput struct {
	FilePath string `json:"file_path"`
}

type HookPayload struct {
	ToolName  string    `json:"tool_name"`
	ToolInput ToolInput `json:"tool_input"`
}

type BlockResponse struct {
	Reason string `json:"reason"`
}

func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checksumKeyPath(filePath string) string {
	h := sha256.Sum256([]byte(filePath))
	return filepath.Join(checksumDir, hex.EncodeToString(h[:]))
}

func store(filePath string) error {
	if _, err := os.Stat(filePath); err != nil {
		return nil // file doesn't exist, nothing to store
	}

	sum, err := fileChecksum(filePath)
	if err != nil {
		return err
	}

	return os.WriteFile(checksumKeyPath(filePath), []byte(sum), 0644)
}

func verify(filePath string) (blocked bool, reason string) {
	if _, err := os.Stat(filePath); err != nil {
		return false, "" // new file, allow
	}

	keyPath := checksumKeyPath(filePath)
	stored, err := os.ReadFile(keyPath)
	if err != nil {
		return false, "" // never read before, allow
	}

	current, err := fileChecksum(filePath)
	if err != nil {
		return false, "" // can't read file, let the tool handle the error
	}

	if string(stored) != current {
		return true, fmt.Sprintf(
			"STALE FILE: %s has been modified externally since it was last read. Re-read the file before editing.",
			filepath.Base(filePath),
		)
	}

	return false, ""
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: file-checksum-guard <verify|store>")
		os.Exit(1)
	}
	action := os.Args[1]

	os.MkdirAll(checksumDir, 0755)

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to read stdin:", err)
		os.Exit(1)
	}

	var payload HookPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		fmt.Fprintln(os.Stderr, "failed to parse JSON:", err)
		os.Exit(1)
	}

	filePath := payload.ToolInput.FilePath
	if filePath == "" {
		os.Exit(0)
	}

	switch action {
	case "store":
		if err := store(filePath); err != nil {
			fmt.Fprintln(os.Stderr, "store error:", err)
			os.Exit(1)
		}

	case "verify":
		if blocked, reason := verify(filePath); blocked {
			resp, _ := json.Marshal(BlockResponse{Reason: reason})
			fmt.Println(string(resp))
			os.Exit(2)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown action: %s\n", action)
		os.Exit(1)
	}
}
