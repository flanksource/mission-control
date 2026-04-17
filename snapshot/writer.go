package snapshot

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/flanksource/incident-commander/utils"
)

func writeToCSVFile(pathPrefix, name string, headerRow string, dbRows []map[string]any) error {
	path, err := utils.SafeJoin(pathPrefix, name)
	if err != nil {
		return err
	}

	f, err := os.Create(path)
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

	path, err := utils.SafeJoin(pathPrefix, name)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func writeToLogFile(pathPrefix, name string, logs []byte) error {
	if len(logs) == 0 {
		return nil
	}

	path, err := utils.SafeJoin(pathPrefix, name)
	if err != nil {
		return err
	}

	return os.WriteFile(path, logs, 0644)
}
