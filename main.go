// Claude Code hook that blocks edits to files modified externally since Claude last read them.
// Invoked as: echo '<json>' | file-checksum-guard store   (after a read)
//
//	echo '<json>' | file-checksum-guard verify  (before an edit)
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const checksumDir = "/tmp/claude-file-checksums"

// Struct tags (`json:"..."`) tell the JSON parser which keys map to which fields.
type ToolInput struct {
	FilePath     string `json:"file_path"`
	RelativePath string `json:"relative_path"`
}

type HookPayload struct {
	ToolName  string    `json:"tool_name"`
	ToolInput ToolInput `json:"tool_input"`
	Cwd       string    `json:"cwd"`
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
	// h.Sum(nil) finalizes the hash and returns raw bytes.
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Hashes the file *path* itself to produce a safe flat filename for storage.
func checksumKeyPath(filePath string) string {
	h := sha256.Sum256([]byte(filePath))
	// `h[:]` converts the fixed-size array ([32]byte) to a slice ([]byte).
	// Go distinguishes between arrays (fixed-size) and slices (dynamic).
	return filepath.Join(checksumDir, hex.EncodeToString(h[:]))
}

func logAccessPath(filePath string) string {
	return checksumKeyPath(filePath) + ".log"
}

// matchStatus: "match", "mismatch", "new" (no prior hash), or "error"
func logAccess(filePath, action, toolName, matchStatus string) {
	entry := fmt.Sprintf("%s  %-6s  tool=%s  hash=%s  file=%s\n",
		time.Now().Format(time.RFC3339), action, toolName, matchStatus, filePath)
	f, err := os.OpenFile(logAccessPath(filePath), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return // best-effort, don't block on log failure
	}
	defer f.Close()
	f.WriteString(entry)
}

// compareWithStored computes the current file hash and compares it to the stored one.
// Returns the match status and the current hash (empty on error).
func compareWithStored(filePath string) (status, currentHash string) {
	current, err := fileChecksum(filePath)
	if err != nil {
		return "error", ""
	}

	stored, err := os.ReadFile(checksumKeyPath(filePath))
	if err != nil {
		return "new", current // no prior hash on record
	}

	if string(stored) == current {
		return "match", current
	}
	return "mismatch", current
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
		return false, "" // can't read file, the tool will also fail to read it, so no need to block here
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

	// `&payload` passes a pointer so Unmarshal can mutate the struct in place.
	var payload HookPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		fmt.Fprintln(os.Stderr, "failed to parse JSON:", err)
		os.Exit(1)
	}

	filePath := payload.ToolInput.FilePath
	if filePath == "" && payload.ToolInput.RelativePath != "" {
		filePath = filepath.Join(payload.Cwd, payload.ToolInput.RelativePath)
	}
	if filePath == "" {
		os.Exit(0)
	}

	switch action {
	case "store":
		status, _ := compareWithStored(filePath)
		if err := store(filePath); err != nil {
			fmt.Fprintln(os.Stderr, "store error:", err)
			os.Exit(1)
		}
		logAccess(filePath, "store", payload.ToolName, status)

	case "verify":
		status, _ := compareWithStored(filePath)
		logAccess(filePath, "verify", payload.ToolName, status)
		if blocked, reason := verify(filePath); blocked {
			// `_` discards the error from Marshal — safe here since
			// BlockResponse is a trivial struct that can't fail to serialize.
			resp, _ := json.Marshal(BlockResponse{Reason: reason})
			fmt.Println(string(resp))
			os.Exit(2) // exit code 2 = "block this tool call" in Claude Code hooks
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown action: %s\n", action)
		os.Exit(1)
	}
}
