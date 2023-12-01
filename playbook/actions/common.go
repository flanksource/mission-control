package actions

import "io"

type ArtifactResult struct {
	ContentType string
	Path        string
	Content     io.ReadCloser
}
