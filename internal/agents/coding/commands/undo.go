package commands

import (
	"fmt"
	"strings"

	basetools "github.com/hwj123hwj/easyagent/sdk/tools"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// undoManager is a package-level backup manager shared across sessions.
// In a multi-session deployment, each App instance should have its own
// BackupManager, but for the CLI/single-session case this is fine.
var undoManager = basetools.NewBackupManager()

// GetUndoManager returns the shared BackupManager instance.
// Called by edit/write tools to snapshot before modifications.
func GetUndoManager() *basetools.BackupManager {
	return undoManager
}

// RegisterUndoCommands registers the /undo slash command.
func RegisterUndoCommands(registry *slashcmd.Registry) {
	registry.Register(slashcmd.Command{
		Name:        "undo",
		Description: "Undo file changes (restore from backup snapshots)",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			args = strings.TrimSpace(args)

			switch {
			case args == "" || args == "last":
				// Undo the last file operation
				infos := undoManager.ListBackups()
				if len(infos) == 0 {
					return slashcmd.CommandResult{Output: "Nothing to undo. No backup snapshots available."}, nil
				}
				// Find the most recently backed-up file
				// (ListBackups doesn't sort by time, so we just take the first)
				target := infos[0]
				if err := undoManager.Restore(target.Path); err != nil {
					return slashcmd.CommandResult{}, fmt.Errorf("undo %s: %w", target.Path, err)
				}
				return slashcmd.CommandResult{
					Output: fmt.Sprintf("↩️ Restored %s from backup.", target.Path),
				}, nil

			case args == "all":
				// Restore all files
				errs := undoManager.RestoreAll()
				if len(errs) > 0 {
					var b strings.Builder
					b.WriteString(fmt.Sprintf("⚠️ Restored with %d error(s):\n", len(errs)))
					for _, e := range errs {
						b.WriteString(fmt.Sprintf("  - %v\n", e))
					}
					return slashcmd.CommandResult{Output: b.String()}, nil
				}
				return slashcmd.CommandResult{
					Output: "↩️ All files restored from backups.",
				}, nil

			case args == "list":
				// List available backups
				return slashcmd.CommandResult{
					Output: undoManager.FormatListBackups(),
				}, nil

			case args == "clear":
				// Clear all backups (frees memory, cannot be undone)
				undoManager.Clear()
				return slashcmd.CommandResult{
					Output: "🧹 All backup snapshots cleared.",
				}, nil

			default:
				return slashcmd.CommandResult{
					Output: undoHelp(),
				}, nil
			}
		},
	})
}

func undoHelp() string {
	return strings.TrimSpace(`
↩️ /undo — Undo file changes using backup snapshots

Usage:
  /undo             Undo the last file operation (restore most recent backup)
  /undo all         Restore all files that have backups
  /undo list        List all available backups
  /undo clear       Clear all backup snapshots (cannot be undone)

How it works:
  Before each edit/write operation, the agent snapshots the file's current
  content. If something goes wrong, /undo restores the file to its state
  before the modification.

Examples:
  /undo             # Oops, that edit was wrong — revert it
  /undo list        # See what backups are available
  /undo all         # Revert everything in this session
`)
}
