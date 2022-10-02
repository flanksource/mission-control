package utils

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/TomOnTime/utfutil"
	"github.com/flanksource/commons/logger"
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
