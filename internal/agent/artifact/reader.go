package artifact

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

type Reader struct{}

func (Reader) Inspect(_ context.Context, local LocalFile) (Digest, error) {
	if strings.TrimSpace(local.Reference.ID) == "" || strings.TrimSpace(local.Path) == "" {
		return Digest{}, ErrInvalid
	}
	info, err := os.Lstat(local.Path)
	if err != nil {
		return Digest{}, fmt.Errorf("inspect conversation artifact file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Digest{}, fmt.Errorf("%w: resolved path is not a regular file", ErrInvalid)
	}
	if info.Size() > MaxFileSize {
		return Digest{}, ErrTooLarge
	}
	file, err := os.Open(local.Path)
	if err != nil {
		return Digest{}, fmt.Errorf("open conversation artifact: %w", err)
	}
	header := make([]byte, 512)
	read, readErr := file.Read(header)
	closeErr := file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return Digest{}, fmt.Errorf("read conversation artifact header: %w", readErr)
	}
	if closeErr != nil {
		return Digest{}, fmt.Errorf("close conversation artifact header: %w", closeErr)
	}
	mediaType := detectMediaType(local.Reference, header[:read])
	kind := detectKind(local.Reference, mediaType)
	digest := Digest{
		ArtifactID: local.Reference.ID,
		Kind:       kind,
		Name:       local.Reference.Name,
		MediaType:  mediaType,
		SizeBytes:  info.Size(),
		Status:     StatusReadable,
	}
	switch kind {
	case KindMarkdown, KindText:
		digest.Text, err = inspectText(local.Path, kind == KindMarkdown)
		if digest.Text != nil && digest.Text.Truncated {
			digest.Status = StatusTruncated
		}
	case KindCSV:
		digest.Table, err = inspectCSV(local.Path)
		if digest.Table != nil && digest.Table.Truncated {
			digest.Status = StatusTruncated
		}
	case KindSpreadsheet:
		digest.Workbook, err = inspectWorkbook(local.Path)
		if digest.Workbook != nil && digest.Workbook.Truncated {
			digest.Status = StatusTruncated
		}
	case KindImage:
		digest.Image, err = inspectImageMetadata(local.Path)
		digest.Status = StatusAnalysisUnavailable
		digest.Warnings = append(digest.Warnings, "Image semantics require the separate multimodal description model.")
	default:
		return Digest{}, ErrUnsupported
	}
	if err != nil {
		return Digest{}, err
	}
	return digest, nil
}

func detectMediaType(reference Reference, header []byte) string {
	declared, _, err := mime.ParseMediaType(strings.TrimSpace(reference.MediaType))
	if err == nil && declared != "" && declared != "application/octet-stream" &&
		!strings.Contains(declared, "*") {
		return declared
	}
	detected := http.DetectContentType(header)
	extension := strings.ToLower(filepath.Ext(reference.Name))
	switch extension {
	case ".md", ".markdown":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	return detected
}

func detectKind(reference Reference, mediaType string) Kind {
	if reference.Kind != "" && reference.Kind != KindFile {
		return reference.Kind
	}
	extension := strings.ToLower(filepath.Ext(reference.Name))
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return KindImage
	case mediaType == "text/markdown" || extension == ".md" || extension == ".markdown":
		return KindMarkdown
	case mediaType == "text/csv" || extension == ".csv":
		return KindCSV
	case mediaType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || extension == ".xlsx":
		return KindSpreadsheet
	case strings.HasPrefix(mediaType, "text/"):
		return KindText
	default:
		return KindFile
	}
}

func inspectText(path string, markdown bool) (*TextDigest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open text artifact: %w", err)
	}
	defer file.Close()
	encoded, err := io.ReadAll(io.LimitReader(file, MaxTextBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read text artifact: %w", err)
	}
	if !utf8.Valid(encoded) {
		return nil, fmt.Errorf("%w: text artifact is not UTF-8", ErrUnsupported)
	}
	sourceTruncated := len(encoded) > MaxTextBytes
	if sourceTruncated {
		encoded = encoded[:MaxTextBytes]
	}
	outputTruncated := len(encoded) > MaxTextOutputBytes
	if outputTruncated {
		encoded = encoded[:MaxTextOutputBytes]
		for !utf8.Valid(encoded) && len(encoded) > 0 {
			encoded = encoded[:len(encoded)-1]
		}
	}
	content := strings.TrimSpace(string(encoded))
	digest := &TextDigest{Content: content, Truncated: sourceTruncated || outputTruncated}
	if markdown {
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() && len(digest.Headings) < 50 {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") {
				heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
				if heading != "" {
					digest.Headings = append(digest.Headings, limitRunes(heading, 200))
				}
			}
		}
	}
	return digest, nil
}

func inspectCSV(path string) (*TableDigest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open CSV artifact: %w", err)
	}
	defer file.Close()
	reader := csv.NewReader(io.LimitReader(file, MaxFileSize+1))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	digest := &TableDigest{}
	for {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("parse CSV artifact: %w", readErr)
		}
		if len(record) > MaxColumns {
			record = record[:MaxColumns]
			digest.Truncated = true
		}
		row := boundedRow(record)
		if digest.RowCount == 0 {
			digest.Columns = append([]string(nil), row...)
		} else if len(digest.SampleRows) < MaxSampleRows {
			digest.SampleRows = append(digest.SampleRows, row)
		}
		digest.RowCount++
		if digest.RowCount >= MaxTableRows {
			digest.Truncated = true
			break
		}
	}
	if digest.RowCount > 0 {
		digest.RowCount--
	}
	return digest, nil
}

func inspectWorkbook(path string) (*WorkbookDigest, error) {
	book, err := excelize.OpenFile(path, excelize.Options{
		RawCellValue: true, UnzipSizeLimit: 64 << 20, UnzipXMLSizeLimit: 16 << 20,
	})
	if err != nil {
		return nil, fmt.Errorf("open spreadsheet artifact: %w", err)
	}
	defer book.Close()
	digest := &WorkbookDigest{}
	totalRows := 0
	for sheetIndex, sheetName := range book.GetSheetList() {
		if sheetIndex >= MaxWorkbookSheets || totalRows >= MaxTableRows {
			digest.Truncated = true
			break
		}
		rows, rowErr := book.Rows(sheetName)
		if rowErr != nil {
			return nil, fmt.Errorf("read spreadsheet sheet %q: %w", sheetName, rowErr)
		}
		sheet := SheetDigest{Name: sheetName}
		for rows.Next() {
			if totalRows >= MaxTableRows {
				sheet.Truncated = true
				break
			}
			columns, columnsErr := rows.Columns(excelize.Options{RawCellValue: true})
			if columnsErr != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("read spreadsheet row in %q: %w", sheetName, columnsErr)
			}
			if len(columns) > MaxColumns {
				columns = columns[:MaxColumns]
				sheet.Truncated = true
			}
			sheet.RowCount++
			totalRows++
			sheet.ColumnCount = max(sheet.ColumnCount, len(columns))
			if len(sheet.SampleRows) < MaxSampleRows {
				row := boundedRow(columns)
				sheet.SampleRows = append(sheet.SampleRows, row)
				for column := range row {
					if len(sheet.FormulaCells) >= 50 {
						break
					}
					cell, nameErr := excelize.CoordinatesToCellName(column+1, sheet.RowCount)
					if nameErr != nil {
						continue
					}
					formula, formulaErr := book.GetCellFormula(sheetName, cell)
					if formulaErr == nil && formula != "" {
						sheet.FormulaCells = append(sheet.FormulaCells, FormulaCell{
							Cell: cell, Formula: limitRunes(formula, MaxCellRunes), Value: row[column],
						})
					}
				}
			}
		}
		if closeErr := rows.Close(); closeErr != nil {
			return nil, fmt.Errorf("close spreadsheet rows in %q: %w", sheetName, closeErr)
		}
		digest.Truncated = digest.Truncated || sheet.Truncated
		digest.Sheets = append(digest.Sheets, sheet)
	}
	return digest, nil
}

func inspectImageMetadata(path string) (*ImageDigest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open image artifact: %w", err)
	}
	configuration, format, err := image.DecodeConfig(file)
	closeErr := file.Close()
	if err != nil {
		return nil, fmt.Errorf("decode image artifact: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close image artifact: %w", closeErr)
	}
	digest := &ImageDigest{Width: configuration.Width, Height: configuration.Height, Format: format}
	return digest, nil
}

func boundedRow(values []string) []string {
	row := make([]string, len(values))
	for index, value := range values {
		row[index] = limitRunes(strings.TrimSpace(value), MaxCellRunes)
	}
	return row
}

func boundedStrings(values []string, maximum, runes int) []string {
	values = slices.Clone(values)
	if len(values) > maximum {
		values = values[:maximum]
	}
	for index := range values {
		values[index] = limitRunes(strings.TrimSpace(values[index]), runes)
	}
	return values
}

func limitRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum]) + "…"
}
