package main

import (
	"runtime"
	"runtime/debug"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// unstamped is what the package globals hold for a build without -ldflags.
var unstamped = buildStamps{Version: unsetVersion, Commit: unsetCommit, Date: unsetDate}

func vcsBuildInfo(revision, buildTime, modified string) *debug.BuildInfo {
	return &debug.BuildInfo{
		GoVersion: "go1.26.0",
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: revision},
			{Key: "vcs.time", Value: buildTime},
			{Key: "vcs.modified", Value: modified},
		},
	}
}

var _ = ginkgo.Describe("faro build info", func() {
	const (
		revision  = "016a5dd66b3c1f9e2d4a8b7c5e0f1a2b3c4d5e6f"
		buildTime = "2026-07-24T09:15:00Z"
	)

	ginkgo.It("prefers the ldflags stamps over the embedded VCS metadata", func() {
		info := newBuildInfo(
			buildStamps{Version: "v1.4.0", Commit: "abcdef0123456789", Date: "2026-07-24T10:30:00Z"},
			vcsBuildInfo(revision, buildTime, "false"),
		)
		Expect(info).To(Equal(BuildInfo{
			Version:  "v1.4.0",
			Commit:   "abcdef0123456789",
			Date:     "2026-07-24T10:30:00Z",
			Go:       "go1.26.0",
			Platform: runtime.GOOS + "/" + runtime.GOARCH,
		}))
	})

	ginkgo.It("recovers the revision and build time from VCS metadata when unstamped", func() {
		info := newBuildInfo(unstamped, vcsBuildInfo(revision, buildTime, "false"))
		Expect(info).To(Equal(BuildInfo{
			Version:  unsetVersion,
			Commit:   revision,
			Date:     buildTime,
			Go:       "go1.26.0",
			Platform: runtime.GOOS + "/" + runtime.GOARCH,
		}))
	})

	ginkgo.It("marks a build made from a modified working tree", func() {
		info := newBuildInfo(unstamped, vcsBuildInfo(revision, buildTime, "true"))
		Expect(info.Dirty).To(BeTrue())
		Expect(info.ShortCommit()).To(Equal("016a5dd6-dirty"))
	})

	ginkgo.It("falls back to the module version when only the version is unstamped", func() {
		bi := vcsBuildInfo(revision, buildTime, "false")
		bi.Main.Version = "v1.3.2"
		Expect(newBuildInfo(unstamped, bi).Version).To(Equal("v1.3.2"))
	})

	ginkgo.It("ignores the placeholder module version of a local build", func() {
		bi := vcsBuildInfo(revision, buildTime, "false")
		bi.Main.Version = "(devel)"
		Expect(newBuildInfo(unstamped, bi).Version).To(Equal(unsetVersion))
	})

	ginkgo.It("reports version and platform when no build info is available", func() {
		info := newBuildInfo(buildStamps{Version: "v1.4.0", Commit: unsetCommit, Date: unsetDate}, nil)
		Expect(info).To(Equal(BuildInfo{
			Version:  "v1.4.0",
			Go:       runtime.Version(),
			Platform: runtime.GOOS + "/" + runtime.GOARCH,
		}))
	})

	ginkgo.It("renders a single line carrying version, revision, date and toolchain", func() {
		info := newBuildInfo(buildStamps{Version: "v1.4.0", Commit: revision, Date: buildTime}, vcsBuildInfo(revision, buildTime, "true"))
		Expect(info.String()).To(Equal(
			"v1.4.0, commit 016a5dd6-dirty, built at 2026-07-24T09:15:00Z (go1.26.0 " + runtime.GOOS + "/" + runtime.GOARCH + ")"))
	})

	ginkgo.It("omits the revision and date from the summary of an unstamped build", func() {
		info := newBuildInfo(unstamped, nil)
		Expect(info.String()).To(Equal(unsetVersion + " (" + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH + ")"))
	})
})

var _ = ginkgo.Describe("faro build date", func() {
	ginkgo.It("appends the age of a parseable build date", func() {
		built := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
		Expect(humanBuildDate(built)).To(Equal(built + " (3.0h ago)"))
	})

	ginkgo.It("passes an unparseable build date through untouched", func() {
		Expect(humanBuildDate(unsetDate)).To(Equal(unsetDate))
	})
})
