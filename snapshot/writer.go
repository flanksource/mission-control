package snapshot

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeToCSVFile(pathPrefix, name string, headerRow string, dbRows []map[string]any) error {
	f, err := os.Create(filepath.Join(pathPrefix, name))
	if err != nil {
		return err
	}

	var rows [][]string
	rows = append(rows, strings.Split(headerRow, ","))
	for _, row := range dbRows {
		var columns []string
		for _, c := range row {
			columns = append(columns, fmt.Sprint(c))
		}
		rows = append(rows, columns)
	}
	w := csv.NewWriter(f)
	return w.WriteAll(rows)
}

func writeToJSONFile(pathPrefix, name string, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	return os.WriteFile(filepath.Join(pathPrefix, name), data, 0644)
}

func writeToLogFile(pathPrefix, name string, logs []byte) error {
	if len(logs) == 0 {
		return nil
	}
	return os.WriteFile(filepath.Join(pathPrefix, name), logs, 0644)
}
