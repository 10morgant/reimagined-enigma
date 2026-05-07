package audit

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func WriteOutput(rows []Row, outputPath string, format OutputFormat) (string, error) {
	switch format {
	case OutputCSV:
		if err := writeCSV(rows, outputPath); err != nil {
			return "", err
		}
		return outputPath, nil
	case OutputConfluence:
		path := outputPath
		if strings.TrimSpace(filepath.Ext(path)) != ".txt" {
			path = strings.TrimSuffix(path, filepath.Ext(path)) + ".txt"
		}
		if err := writeConfluence(rows, path); err != nil {
			return "", err
		}
		return path, nil
	default:
		return "", fmt.Errorf("unsupported output format %q", format)
	}
}

func writeCSV(rows []Row, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{"FileName", "FullPath", "ProjectFolder", "Owner", "Size", "Created", "LastModified", "Age", "DaysSinceModified"}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		record := []string{row.FileName, row.FullPath, row.ProjectFolder, row.Owner, row.Size, row.Created, row.LastModified, row.Age, row.DaysSinceModified}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeConfluence(rows []Row, outputPath string) error {
	headers := []string{"FileName", "FullPath", "ProjectFolder", "Owner", "Size", "Created", "LastModified", "Age", "DaysSinceModified", "ActionRequired", "Notes"}
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, "||"+strings.Join(headers, "||")+"||")

	for _, row := range rows {
		vals := []string{
			escapeConfluence(row.FileName),
			escapeConfluence(row.FullPath),
			escapeConfluence(row.ProjectFolder),
			escapeConfluence(row.Owner),
			escapeConfluence(row.Size),
			escapeConfluence(row.Created),
			escapeConfluence(row.LastModified),
			escapeConfluence(row.Age),
			escapeConfluence(row.DaysSinceModified),
			"",
			"",
		}
		lines = append(lines, "|"+strings.Join(vals, "|")+"|")
	}
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(outputPath, []byte(content), 0o644)
}

func escapeConfluence(v string) string {
	return strings.ReplaceAll(v, "|", `\|`)
}
