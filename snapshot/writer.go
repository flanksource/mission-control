package snapshot

import (
	"bufio"
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
