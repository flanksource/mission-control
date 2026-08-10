package main

import (
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/spf13/cobra"
)

var (
	projectionFile   string
	projectionName   string
	projectionDryRun bool
)

type ProjectionRunResult struct {
	Projection string              `json:"projection" yaml:"projection"`
	Context    map[string]any      `json:"context" yaml:"context"`
	Items      []map[string]any    `json:"items" yaml:"items"`
	Warnings   []ProjectionWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type ProjectionVerifyResult struct {
	File        string   `json:"file" yaml:"file"`
	Projections []string `json:"projections" yaml:"projections"`
}

var ProjectionRun = &cobra.Command{
	Use:   "run",
	Short: "Run Projection source queries",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		projections, err := selectedProjectionDocuments()
		if err != nil {
			return err
		}
		results := make([]ProjectionRunResult, 0, len(projections))
		for _, projection := range projections {
			source, err := runProjectionQuery(projection)
			if err != nil {
				return err
			}
			results = append(results, ProjectionRunResult{
				Projection: projection.Metadata.Name,
				Context:    source.Context,
				Items:      source.Items,
				Warnings:   source.Warnings,
			})
		}
		clicky.MustPrint(results, clicky.Flags.FormatOptions)
		return nil
	},
}

var ProjectionApply = &cobra.Command{
	Use:   "apply",
	Short: "Apply Projection mappings to their targets",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		projections, err := selectedProjectionDocuments()
		if err != nil {
			return err
		}
		results := make([]ProjectionApplyResult, 0, len(projections))
		for _, projection := range projections {
			if projection.Spec.Target == nil {
				continue
			}
			source, err := runProjectionQuery(projection)
			if err != nil {
				return err
			}
			result, err := applyProjection(projection, source, projectionDryRun)
			if err != nil {
				return err
			}
			results = append(results, result)
		}
		if len(results) == 0 {
			return fmt.Errorf("no selected Projection document has a target")
		}
		clicky.MustPrint(results, clicky.Flags.FormatOptions)
		return nil
	},
}

var ProjectionVerify = &cobra.Command{
	Use:   "verify",
	Short: "Validate Projection documents and targets without querying Mission Control",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		projections, err := selectedProjectionDocuments()
		if err != nil {
			return err
		}
		result := ProjectionVerifyResult{File: projectionFile, Projections: make([]string, 0, len(projections))}
		for _, projection := range projections {
			if err := verifyProjection(projection); err != nil {
				return err
			}
			result.Projections = append(result.Projections, projection.Metadata.Name)
		}
		clicky.MustPrint(result, clicky.Flags.FormatOptions)
		return nil
	},
}

func selectedProjectionDocuments() ([]Projection, error) {
	if projectionFile == "" {
		return nil, fmt.Errorf("--file is required")
	}
	projections, err := loadProjections(projectionFile)
	if err != nil {
		return nil, err
	}
	if err := validateProjectionNames(projections); err != nil {
		return nil, err
	}
	return selectProjections(projections, projectionName)
}

func validateProjectionNames(projections []Projection) error {
	seen := map[string]struct{}{}
	for _, projection := range projections {
		if _, ok := seen[projection.Metadata.Name]; ok {
			return fmt.Errorf("duplicate projection metadata.name %q", projection.Metadata.Name)
		}
		seen[projection.Metadata.Name] = struct{}{}
	}
	return nil
}

func bindProjectionFlags(command *cobra.Command) {
	command.Flags().StringVarP(&projectionFile, "file", "f", "", "Path to a multi-document Projection YAML file")
	command.Flags().StringVar(&projectionName, "name", "", "Run only the Projection with this metadata.name")
	clicky.BindAllFlags(command.PersistentFlags(), "format")
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
