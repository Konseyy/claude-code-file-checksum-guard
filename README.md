# file-checksum-guard

A Claude Code hook that prevents edits to stale files — files modified externally since Claude last read them.

## Problem

Claude reads a file, you edit it in your IDE, then Claude edits based on the old content — overwriting your changes. This hook blocks that.

## How it works

The binary runs in two modes:

- **`store`** — After a file read/write/edit, computes a SHA-256 checksum and saves it to `/tmp/claude-file-checksums/`.
- **`verify`** — Before an edit, compares the current checksum to the stored one. If they differ, the edit is blocked and Claude is told to re-read the file.

## Hook configuration

In `~/.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "/home/{user}/.claude/hooks/file-checksum-guard/file-checksum-guard verify"
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Read|Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "/home/{user}/.claude/hooks/file-checksum-guard/file-checksum-guard store"
          }
        ]
      }
    ]
  }
}
```

## Build

```sh
go build -o file-checksum-guard .
```
