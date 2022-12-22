package snapshot

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
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

func writeToJSONFile(pathPrefix, name string, data []map[string]any) error {
	if len(data) == 0 {
		return nil
	}

	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(pathPrefix, name), b, 0644)
}

func writeToLogFile(pathPrefix, name string, logs []json.RawMessage) error {
	f, err := os.Create(filepath.Join(pathPrefix, name))
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)

	for _, log := range logs {
		_, err = w.WriteString(string(log) + "\n")
		if err != nil {
			return err
		}
	}
	w.Flush()

	return nil
}
