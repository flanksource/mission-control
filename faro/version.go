package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/spf13/cobra"
)

// Sentinels for a build that carries no -ldflags stamps; the values are then
// recovered from the VCS metadata the Go toolchain embeds.
const (
	unsetVersion = "dev"
	unsetCommit  = "none"
	unsetDate    = "unknown"
)

// Stamped by the Makefile via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = unsetVersion
	commit  = unsetCommit
	date    = unsetDate
)

// buildStamps holds the -ldflags values, passed explicitly so the resolution
// logic can be tested without mutating package state.
type buildStamps struct {
	Version string
	Commit  string
	Date    string
}

// BuildInfo describes the running faro binary.
type BuildInfo struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Dirty    bool   `json:"dirty,omitempty"`
	Date     string `json:"date,omitempty"`
	Go       string `json:"go"`
	Platform string `json:"platform"`
}

// newBuildInfo prefers the -ldflags stamps and falls back to the VCS metadata
// Go embeds when building from a checkout, so `go build ./faro` and
// `go install` still report a real revision. bi may be nil.
func newBuildInfo(stamps buildStamps, bi *debug.BuildInfo) BuildInfo {
	info := BuildInfo{
		Version:  stamps.Version,
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
	if stamps.Commit != unsetCommit {
		info.Commit = stamps.Commit
	}
	if stamps.Date != unsetDate {
		info.Date = stamps.Date
	}
	if bi == nil {
		return info
	}

	if info.Version == unsetVersion && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.Version = bi.Main.Version
	}
	if bi.GoVersion != "" {
		info.Go = bi.GoVersion
	}
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = setting.Value
			}
		case "vcs.time":
			if info.Date == "" {
				info.Date = setting.Value
			}
		case "vcs.modified":
			info.Dirty = setting.Value == "true"
		}
	}
	return info
}

func currentBuildInfo() BuildInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		bi = nil
	}
	return newBuildInfo(buildStamps{Version: version, Commit: commit, Date: date}, bi)
}

// ShortCommit is the abbreviated revision, marked when the tree was dirty.
func (b BuildInfo) ShortCommit() string {
	if b.Commit == "" {
		return ""
	}
	short := b.Commit
	if len(short) > 8 {
		short = short[:8]
	}
	if b.Dirty {
		return short + "-dirty"
	}
	return short
}

// String is the single-line form used for the usage footer and the User-Agent.
func (b BuildInfo) String() string {
	s := b.Version
	if c := b.ShortCommit(); c != "" {
		s += ", commit " + c
	}
	if b.Date != "" {
		s += ", built at " + b.Date
	}
	return fmt.Sprintf("%s (%s %s)", s, b.Go, b.Platform)
}

func (b BuildInfo) Pretty() api.Text {
	items := []api.KeyValuePair{}
	if c := b.ShortCommit(); c != "" {
		items = append(items, api.KeyValuePair{Key: "Commit", Value: c})
	}
	if b.Date != "" {
		items = append(items, api.KeyValuePair{Key: "Built", Value: humanBuildDate(b.Date)})
	}
	items = append(items,
		api.KeyValuePair{Key: "Go", Value: b.Go},
		api.KeyValuePair{Key: "Platform", Value: b.Platform},
	)

	return clicky.Text("faro "+b.Version, "font-bold").
		NewLine().
		Append(api.DescriptionList{Items: items}).
		NewLine()
}

// humanBuildDate appends an age to the timestamp when it parses, so a stale
// binary is obvious at a glance.
func humanBuildDate(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return fmt.Sprintf("%s (%s ago)", raw, api.Human(time.Since(t)).String())
}

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version, git revision and build date of faro",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			clicky.MustPrint(currentBuildInfo(), clicky.Flags.FormatOptions)
		},
	}
	clicky.BindAllFlags(cmd.Flags(), "format")
	return cmd
}
