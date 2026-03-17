package mcp

import (
	gocontext "context"
	"fmt"
	"io"

	"github.com/flanksource/artifacts"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/flanksource/incident-commander/db"
)

const (
	defaultMaxLength = 1048576 // 1 MB

	toolReadArtifactMetadata = "read_artifact_metadata"
	toolReadArtifactContent  = "read_artifact_content"
)

func readArtifactMetadataHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	artifactIDStr, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	artifactID, err := uuid.Parse(artifactIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid id(%s). must be a uuid: %v", artifactIDStr, err)), nil
	}

	artifact, err := db.FindArtifact(ctx, artifactID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	} else if artifact == nil {
		return mcp.NewToolResultError(fmt.Sprintf("artifact(%s) was not found", artifactID)), nil
	}

	return structToMCPResponse(req, []*models.Artifact{artifact}), nil
}

func readArtifactContentHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	artifactIDStr, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	artifactID, err := uuid.Parse(artifactIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid id(%s). must be a uuid: %v", artifactIDStr, err)), nil
	}

	maxLength := req.GetInt("max_length", defaultMaxLength)

	artifact, err := db.FindArtifact(ctx, artifactID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	} else if artifact == nil {
		return mcp.NewToolResultError(fmt.Sprintf("artifact(%s) was not found", artifactID)), nil
	}

	conn, err := pkgConnection.Get(ctx, artifact.ConnectionID.String())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	} else if conn == nil {
		return mcp.NewToolResultError("artifact's connection was not found"), nil
	}

	fs, err := artifacts.GetFSForConnection(ctx, *conn)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer fs.Close()

	file, err := fs.Read(ctx, artifact.Path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer file.Close()

	// Read up to maxLength bytes
	buffer := make([]byte, maxLength)
	n, err := io.ReadFull(file, buffer)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return mcp.NewToolResultError(err.Error()), nil
	}

	content := string(buffer[:n])

	// Check if content was truncated
	if n == maxLength {
		// Try to read one more byte to see if there's more content
		oneByte := make([]byte, 1)
		_, err := file.Read(oneByte)
		if err == nil {
			// There's more content, so it was truncated
			content = fmt.Sprintf("%s\n\n[Content truncated. Artifact size: %d bytes, showing first %d bytes. Use a larger max_length to see more.]",
				content, artifact.Size, maxLength)
		}
	}

	return mcp.NewToolResultText(content), nil
}

func registerArtifacts(s *server.MCPServer) {
	readArtifactMetadataTool := mcp.NewTool(toolReadArtifactMetadata,
		mcp.WithDescription("Get artifact metadata by ID including filename, size, content type, path, check/playbook run association, and timestamps"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("UUID of the artifact"),
		),
	)

	readArtifactContentTool := mcp.NewTool(toolReadArtifactContent,
		mcp.WithDescription("Read the actual content of an artifact file. Content will be truncated if it exceeds max_length."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("UUID of the artifact"),
		),
		mcp.WithNumber("max_length",
			mcp.Description("Maximum number of bytes to read from the artifact. Default: 1048576 (1 MB). Content will be truncated with a note if the artifact is larger."),
		),
	)

	s.AddTool(readArtifactMetadataTool, readArtifactMetadataHandler)
	s.AddTool(readArtifactContentTool, readArtifactContentHandler)
}
