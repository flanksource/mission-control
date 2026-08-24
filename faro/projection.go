package main

import (
	"fmt"
	"slices"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/spf13/cobra"
)

var (
	projectionFiles  []string
	projectionName   string
	projectionDryRun bool
)

type ProjectionRunResult struct {
	Projection string              `json:"projection" yaml:"projection"`
	Context    map[string]any      `json:"context" yaml:"context"`
	Items      []map[string]any    `json:"items" yaml:"items"`
	Filtered   int                 `json:"filtered" yaml:"filtered"`
	Warnings   []ProjectionWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type ProjectionVerifyResult struct {
	Files       []string `json:"files" yaml:"files"`
	Projections []string `json:"projections" yaml:"projections"`
}

// run reports source selection only, but compiles the whole manifest on the way there:
// selectProjectionSources builds every spec.set program even though only spec.source.where
// is evaluated here. That is deliberate — a mapping that does not compile is worth hearing
// about before a live query rather than after one — so a broken spec.set fails run too.
var ProjectionRun = &cobra.Command{
	Use:   "run [path...]",
	Short: "Run Projection source queries, compiling the whole manifest",
	RunE: func(cmd *cobra.Command, args []string) error {
		projections, err := selectedProjectionDocuments(args)
		if err != nil {
			return err
		}
		results := make([]ProjectionRunResult, 0, len(projections))
		for _, projection := range projections {
			source, err := runProjectionQuery(projection)
			if err != nil {
				return err
			}
			selected, filtered, err := selectProjectionSources(projection, source)
			if err != nil {
				return err
			}
			results = append(results, ProjectionRunResult{
				Projection: projection.Metadata.Name,
				Context:    source.Context,
				Items:      selected,
				Filtered:   filtered,
				Warnings:   source.Warnings,
			})
		}
		clicky.MustPrint(results, clicky.Flags.FormatOptions)
		return nil
	},
}

var ProjectionApply = &cobra.Command{
	Use:   "apply [path...]",
	Short: "Apply Projection mappings to their targets",
	RunE: func(cmd *cobra.Command, args []string) error {
		clicky.Flags.Apply()
		projections, err := selectedProjectionDocuments(args)
		if err != nil {
			return err
		}
		if err := preflightProjections(projections); err != nil {
			return err
		}
		results := applyProjections(projections, runProjectionQuery, projectionDryRun)
		if !slices.ContainsFunc(results, func(r ProjectionApplyResult) bool { return r.Status != projectionSkipped }) {
			return fmt.Errorf("no selected Projection document has a target")
		}
		// Failures first, summary last: the table is what should still be on screen when
		// the command returns.
		if report := projectionFailureReport(results); report != nil && rendersPretty(clicky.Flags.FormatOptions) {
			clicky.MustPrint(report, clicky.Flags.FormatOptions)
		}
		clicky.MustPrint(results, clicky.Flags.FormatOptions)
		if failed := failedProjections(results); failed > 0 {
			return fmt.Errorf("%d of %d projections failed", failed, len(results))
		}
		return nil
	},
}

// rendersPretty reports whether output is the human-readable form. The failure report is
// terminal decoration; a machine format already carries each result's error field and
// must not have prose appended to it.
func rendersPretty(options clicky.FormatOptions) bool {
	if options.JSON || options.YAML || options.CSV || options.Markdown || options.HTML || options.PDF || options.Slack {
		return false
	}
	return options.Format == "" || options.Format == "pretty"
}

// projectionQueryFunc is the Mission Control round trip, injected so the apply
// orchestration can be exercised without one.
type projectionQueryFunc func(Projection) (projectionSourceResult, error)

// applyProjections runs every projection to completion, isolating each failure to its
// own result. It deliberately returns no error: a projection that fails must not hide
// the ones that already wrote their targets, so the caller derives the exit status from
// the statuses instead.
//
// Projections run one at a time. Several of them can name the same target file, and
// applyProjection reads, edits and rewrites that whole file, so concurrent application
// to a shared target would silently drop one projection's writes.
func applyProjections(projections []Projection, query projectionQueryFunc, dryRun bool) []ProjectionApplyResult {
	group := clicky.StartGroup[ProjectionApplyResult]("Applying projections", task.WithConcurrency(1))
	handles := make([]task.TypedTask[ProjectionApplyResult], 0, len(projections))
	for index, projection := range projections {
		handles = append(handles, group.Add(
			fmt.Sprintf("[%d/%d] %s", index+1, len(projections), projection.Metadata.Name),
			func(_ flanksourceContext.Context, t *task.Task) (ProjectionApplyResult, error) {
				return applyOneProjection(projection, query, dryRun, t)
			},
		))
	}

	// Not group.WaitFor()/GetResults(): both return on the first child error, which is
	// exactly the abort this function exists to remove.
	results := make([]ProjectionApplyResult, 0, len(handles))
	for index, handle := range handles {
		result, err := handle.GetResult()
		if err != nil {
			result.Projection = projections[index].Metadata.Name
			result.Status = projectionFailed
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results
}

func applyOneProjection(
	projection Projection,
	query projectionQueryFunc,
	dryRun bool,
	t *task.Task,
) (ProjectionApplyResult, error) {
	if projection.Spec.Target == nil {
		t.Infof("no target; nothing to apply")
		return ProjectionApplyResult{Projection: projection.Metadata.Name, Status: projectionSkipped, DryRun: dryRun}, nil
	}
	t.Infof("querying %s", projectionSourceKind(projection))
	source, err := query(projection)
	if err != nil {
		return ProjectionApplyResult{Projection: projection.Metadata.Name}, err
	}
	t.Infof("%d sources", len(source.Items))

	result, err := applyProjection(projection, source, dryRun)
	if err != nil {
		return result, err
	}
	result.Status = projectionApplied
	for _, warning := range result.Warnings {
		t.Warnf("%s: %s", warning.Source, warning.Message)
	}
	if len(result.Warnings) > 0 {
		result.Status = projectionWarned
		t.Warning()
	}
	t.Infof("%d matched, %d created, %d changed, %d stale", result.Matched, len(result.Created), len(result.Changed), len(result.Stale))
	return result, nil
}

func failedProjections(results []ProjectionApplyResult) int {
	failed := 0
	for _, result := range results {
		if result.Status == projectionFailed {
			failed++
		}
	}
	return failed
}

// preflightProjections fails the whole run on the errors that are properties of the
// manifests rather than of the data: a mistyped CEL expression or an unselected Mission
// Control context is identical for every projection, and reporting it once with nothing
// written beats reporting it N times after half the registers have changed.
func preflightProjections(projections []Projection) error {
	for _, projection := range projections {
		if _, err := compileProjection(projection); err != nil {
			return fmt.Errorf("projection %s: %w", projection.Metadata.Name, err)
		}
	}
	if _, err := accessContextName(); err != nil {
		return err
	}
	return nil
}

var ProjectionVerify = &cobra.Command{
	Use:   "verify [path...]",
	Short: "Validate Projection documents and targets without querying Mission Control",
	RunE: func(cmd *cobra.Command, args []string) error {
		projections, err := selectedProjectionDocuments(args)
		if err != nil {
			return err
		}
		// The manifests actually verified, not the paths asked for: a directory
		// argument is only useful in the report once it names the files it covered.
		result := ProjectionVerifyResult{Projections: make([]string, 0, len(projections))}
		files := map[string]struct{}{}
		for _, projection := range projections {
			if err := verifyProjection(projection); err != nil {
				return err
			}
			if _, ok := files[projection.manifest]; !ok {
				files[projection.manifest] = struct{}{}
				result.Files = append(result.Files, projection.manifest)
			}
			result.Projections = append(result.Projections, projection.Metadata.Name)
		}
		clicky.MustPrint(result, clicky.Flags.FormatOptions)
		return nil
	},
}

// projectionPaths is every manifest path the invocation names, from repeated --file
// flags and from positional arguments, so `faro projection verify a.yaml b.yaml` and
// `--file dir/` and a shell glob all reach the loader the same way.
func projectionPaths(args []string) []string {
	return append(append([]string{}, projectionFiles...), args...)
}

func selectedProjectionDocuments(args []string) ([]Projection, error) {
	paths := projectionPaths(args)
	if len(paths) == 0 {
		return nil, fmt.Errorf("a Projection file or directory is required, as an argument or --file")
	}
	projections, err := loadProjectionPaths(paths)
	if err != nil {
		return nil, err
	}
	if err := validateProjectionNames(projections); err != nil {
		return nil, err
	}
	return selectProjections(projections, projectionName)
}

// Names must stay unique across every loaded manifest, not just within one file:
// --name selects by name alone, and two files claiming it would make that ambiguous.
func validateProjectionNames(projections []Projection) error {
	seen := map[string]Projection{}
	for _, projection := range projections {
		if previous, ok := seen[projection.Metadata.Name]; ok {
			return fmt.Errorf("duplicate projection metadata.name %q in %s and %s",
				projection.Metadata.Name, previous.manifest, projection.manifest)
		}
		seen[projection.Metadata.Name] = projection
	}
	return nil
}

func bindProjectionFlags(command *cobra.Command) {
	command.Args = cobra.ArbitraryArgs
	command.Flags().StringArrayVarP(&projectionFiles, "file", "f", nil,
		"Path to a Projection YAML file or a directory of them; repeatable, and also accepted as positional arguments")
	command.Flags().StringVar(&projectionName, "name", "", "Run only the Projection with this metadata.name")
	// "tasks" carries --no-progress for non-interactive runs. --max-concurrent comes
	// with it but cannot widen apply past one projection at a time: projections sharing
	// a target file must not be applied concurrently.
	clicky.BindAllFlags(command.PersistentFlags(), "format", "tasks")
}

func init() {
	bindProjectionFlags(ProjectionRun)
	bindProjectionFlags(ProjectionApply)
	bindProjectionFlags(ProjectionVerify)
	ProjectionApply.Flags().BoolVar(&projectionDryRun, "dry-run", false, "Preview changes without writing target files")
	clicky.RegisterSubCommand("projection", ProjectionRun)
	clicky.RegisterSubCommand("projection", ProjectionApply)
	clicky.RegisterSubCommand("projection", ProjectionVerify)
}
