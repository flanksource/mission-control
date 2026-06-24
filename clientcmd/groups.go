package clientcmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// Command group IDs used to organize the top-level --help output into titled
// sections. The dynamic playbook and plugin commands are tagged with their
// respective IDs as they are registered; any remaining top-level command (the
// static client, catalog, version and server commands) is folded into the core
// group by FinalizeCommandGroups.
const (
	GroupCore      = "core"
	GroupPlaybooks = "playbooks"
	GroupPlugins   = "plugins"
)

// commandGroupTitles maps a group ID to the heading cobra renders for it.
// Titles carry a trailing colon to match the existing "Logging flags:" /
// "Format flags:" flag-section style.
var commandGroupTitles = map[string]string{
	GroupCore:      "Core Commands:",
	GroupPlaybooks: "Playbook Commands:",
	GroupPlugins:   "Plugin Commands:",
}

// ensureCommandGroup registers groupID on parent, idempotently. Commands are
// tagged with groupID before being attached, and cobra panics in
// checkCommandGroups if the group does not exist on the parent, so this must be
// called whenever a grouped command is added.
func ensureCommandGroup(parent *cobra.Command, groupID string) {
	if parent == nil || groupID == "" {
		return
	}
	if parent.ContainsGroup(groupID) {
		return
	}
	title, ok := commandGroupTitles[groupID]
	if !ok {
		title = groupID
	}
	parent.AddGroup(&cobra.Group{ID: groupID, Title: title})
}

// normalizeShort trims surrounding whitespace from a remote description before
// it is used as a cobra command's Short field, so multi-line / padded
// descriptions don't render verbatim in the --help command listing. The
// per-command Long help keeps the full description.
func normalizeShort(s string) string {
	return strings.TrimSpace(s)
}

// FinalizeCommandGroups folds every remaining top-level command that has no
// explicit group (the static client, catalog, version and server commands),
// plus the auto-generated help and completion commands, into the core group so
// --help renders clean sections with no "Additional Commands" bucket.
//
// Grouping is only enabled when at least one dynamic command was registered
// (plugin or playbook): without dynamic commands the help output keeps its
// original flat "Available Commands" form. The core group itself is registered
// by the dynamic command registration (always before the dynamic groups) so the
// Core section always renders first.
func FinalizeCommandGroups(root *cobra.Command) {
	if root == nil {
		return
	}
	if !root.ContainsGroup(GroupPlugins) && !root.ContainsGroup(GroupPlaybooks) {
		return
	}
	ensureCommandGroup(root, GroupCore)
	root.SetHelpCommandGroupID(GroupCore)
	root.SetCompletionCommandGroupID(GroupCore)
	for _, cmd := range root.Commands() {
		if cmd.GroupID == "" {
			cmd.GroupID = GroupCore
		}
	}
}
