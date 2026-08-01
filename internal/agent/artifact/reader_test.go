package artifact

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestReaderInspectsMarkdownAndCSV(t *testing.T) {
	directory := t.TempDir()
	markdownPath := filepath.Join(directory, "design.md")
	require.NoError(t, os.WriteFile(markdownPath, []byte("# Design\n\n## Risks\n\n- Access\n"), 0o600))
	csvPath := filepath.Join(directory, "accounts.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("id,rate\na,0.2\nb,0.4\n"), 0o600))
	reader := Reader{}

	markdown, err := reader.Inspect(context.Background(), LocalFile{
		Reference: Reference{ID: "md", Kind: KindMarkdown, Name: "design.md", MediaType: "text/markdown"},
		Path:      markdownPath,
	})
	require.NoError(t, err)
	require.Equal(t, StatusReadable, markdown.Status)
	require.Equal(t, []string{"Design", "Risks"}, markdown.Text.Headings)

	table, err := reader.Inspect(context.Background(), LocalFile{
		Reference: Reference{ID: "csv", Kind: KindCSV, Name: "accounts.csv", MediaType: "text/csv"},
		Path:      csvPath,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"id", "rate"}, table.Table.Columns)
	require.Equal(t, 2, table.Table.RowCount)
	require.Equal(t, [][]string{{"a", "0.2"}, {"b", "0.4"}}, table.Table.SampleRows)
}

func TestReaderInspectsWorkbookSheetsAndFormulasWithoutCalculating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.xlsx")
	book := excelize.NewFile()
	require.NoError(t, book.SetCellValue("Sheet1", "A1", "Value"))
	require.NoError(t, book.SetCellValue("Sheet1", "A2", 2))
	require.NoError(t, book.SetCellFormula("Sheet1", "A3", "SUM(A2:A2)"))
	_, err := book.NewSheet("Future")
	require.NoError(t, err)
	require.NoError(t, book.SetCellValue("Future", "A1", "Not agreed"))
	require.NoError(t, book.SaveAs(path))
	require.NoError(t, book.Close())

	digest, err := (Reader{}).Inspect(context.Background(), LocalFile{
		Reference: Reference{
			ID: "xlsx", Kind: KindSpreadsheet, Name: "plan.xlsx",
			MediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		},
		Path: path,
	})

	require.NoError(t, err)
	require.Len(t, digest.Workbook.Sheets, 2)
	require.Equal(t, "Sheet1", digest.Workbook.Sheets[0].Name)
	require.Equal(t, "SUM(A2:A2)", digest.Workbook.Sheets[0].FormulaCells[0].Formula)
	require.Equal(t, "Future", digest.Workbook.Sheets[1].Name)
}

func TestReaderReportsImageAnalysisUnavailableWithoutVisionModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.png")
	file, err := os.Create(path)
	require.NoError(t, err)
	canvas := image.NewRGBA(image.Rect(0, 0, 40, 20))
	canvas.Set(0, 0, color.Black)
	require.NoError(t, png.Encode(file, canvas))
	require.NoError(t, file.Close())

	digest, err := (Reader{}).Inspect(context.Background(), LocalFile{
		Reference: Reference{ID: "image", Kind: KindImage, Name: "report.png", MediaType: "image/*"},
		Path:      path,
	})

	require.NoError(t, err)
	require.Equal(t, StatusAnalysisUnavailable, digest.Status)
	require.Equal(t, "image/png", digest.MediaType)
	require.Equal(t, 40, digest.Image.Width)
	require.Empty(t, digest.Image.Summary)
}

func TestReaderRejectsNonRegularAndOversizedFiles(t *testing.T) {
	reader := Reader{}
	_, err := reader.Inspect(context.Background(), LocalFile{
		Reference: Reference{ID: "directory", Kind: KindText}, Path: t.TempDir(),
	})
	require.ErrorIs(t, err, ErrInvalid)

	path := filepath.Join(t.TempDir(), "large.txt")
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(MaxFileSize+1))
	require.NoError(t, file.Close())
	_, err = reader.Inspect(context.Background(), LocalFile{
		Reference: Reference{ID: "large", Kind: KindText, Name: "large.txt"}, Path: path,
	})
	require.ErrorIs(t, err, ErrTooLarge)
}
