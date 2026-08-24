package main

import (
	"errors"
	"fmt"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/goccy/go-yaml"
	"github.com/samber/oops"
)

// projectionYAMLError converts a goccy/go-yaml error into an oops error carrying the
// token position as structured context.
//
// goccy's yaml.Error implementations (SyntaxError, TypeError, DuplicateKeyError, ...) hold a
// *token.Token, and goccy tokens are a doubly linked list spanning every token in the document.
// Any reflection-based renderer — Gomega's failure formatter, spew, a struct logger — walks that
// list and prints the whole stream. The position, the offending token and goccy's annotated
// source excerpt are read here and stored as scalars, and the original error is deliberately not
// wrapped so the token graph stays unreachable from the returned error.
func projectionYAMLError(err error) error {
	if err == nil {
		return nil
	}
	var yamlErr yaml.Error
	if !errors.As(err, &yamlErr) {
		return err
	}
	builder := oops.Code(dutyAPI.EINVALID).With("yaml.type", fmt.Sprintf("%T", yamlErr))
	if token := yamlErr.GetToken(); token != nil {
		builder = builder.With(
			"yaml.line", token.Position.Line,
			"yaml.column", token.Position.Column,
			"yaml.offset", token.Position.Offset,
			"yaml.indent", token.Position.IndentNum,
			"yaml.token", token.Value,
		)
	}
	return builder.Errorf("%s", yamlErr.FormatError(false, true))
}
