package utils

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/TomOnTime/utfutil"
	"github.com/flanksource/commons/logger"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

func BytesToUtf8Lf(file []byte) (string, error) {
	decoded := utfutil.BytesReader(file, utfutil.UTF8)
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(decoded)
	if err != nil {
		logger.Errorf("error reading from buffer: %v", err)
		return "", err
	}
	val := buf.Bytes()
	// replace \r with \n -> solves for Mac but leaves \n\n for Windows
	val = bytes.ReplaceAll(val, []byte{13}, []byte{10})
	// replace \n\n with \n
	val = bytes.ReplaceAll(val, []byte{10, 10}, []byte{10})
	return string(val), nil
}

func GetUnstructuredObjects(data []byte) ([]unstructured.Unstructured, error) {
	utfData, err := BytesToUtf8Lf(data)
	if err != nil {
		return nil, fmt.Errorf("error converting to UTF %v", err)
	}
	var items []unstructured.Unstructured
	re := regexp.MustCompile(`(?m)^---\n`)
	for _, chunk := range re.Split(utfData, -1) {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(chunk)), 1024)
		var resource *unstructured.Unstructured

		if err := decoder.Decode(&resource); err != nil {
			return nil, fmt.Errorf("error decoding %s: %s", chunk, err)
		}
		if resource != nil {
			items = append(items, *resource)
		}
	}

	return items, nil
}

func MarkdownToHTML(md string) string {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse([]byte(md))

	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)
	return string(markdown.Render(doc, renderer))
}

func ParseTime(t string) *time.Time {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		time.ANSIC,
		time.DateTime,
		time.DateOnly,
		"2006-01-02T15:04:05", // ISO8601 without timezone
		"2006-01-02 15:04:05", // MySQL datetime format
	}

	for _, format := range formats {
		parsed, err := time.Parse(format, t)
		if err == nil {
			return &parsed
		}
	}

	return nil
}
