package snapshot

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
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
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath.Join(pathPrefix, name), b, 0644)
}

func archive(src, dst string) error {
	return nil
}
