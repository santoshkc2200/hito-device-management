package cliimport

import (
	"encoding/csv"
	"fmt"
	"io"
)

// readCSVRows reads a header row plus data rows, requiring every column in
// required to be present in the header (in any order — extra columns are
// ignored). Each returned row is a header-name -> value map, so callers
// never depend on column order. Returns an error immediately if the file
// is malformed or missing a required column; never partially succeeds.
func readCSVRows(r io.Reader, required []string) ([]map[string]string, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	index := make(map[string]int, len(header))
	for i, name := range header {
		index[name] = i
	}
	for _, name := range required {
		if _, ok := index[name]; !ok {
			return nil, fmt.Errorf("missing required column %q", name)
		}
	}

	var rows []map[string]string
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		row := make(map[string]string, len(index))
		for name, i := range index {
			if i < len(record) {
				row[name] = record[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
