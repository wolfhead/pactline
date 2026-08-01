package artifact

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

func TestLLMDescriberMakesOneCallWithBoundedCSVSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.csv")
	var source strings.Builder
	source.WriteString("id,value\n")
	for index := 0; index < 5_000; index++ {
		source.WriteString("account,1234567890\n")
	}
	require.NoError(t, writeTestFile(path, source.String()))
	model := &descriptionModel{response: "The supplied leading-row sample repeats the same value; it is not full-dataset proof."}
	describer := &LLMDescriber{Model: model}

	description, err := describer.Describe(context.Background(), LocalFile{
		Reference: Reference{ID: "csv", Kind: KindCSV, Name: "large.csv", MediaType: "text/csv"},
		Path:      path,
	}, "Determine whether the file proves the full affected scope.")

	require.NoError(t, err)
	require.Contains(t, description, "not full-dataset proof")
	require.Equal(t, 1, model.calls)
	require.Len(t, model.inputs, 1)
	require.Contains(t, model.inputs[0][0].Content, "not completeness of the underlying business population")
	require.Contains(t, model.inputs[0][0].Content, "do not incorrectly call the parser view partial")
	prompt := model.inputs[0][1].Content
	require.Contains(t, prompt, `"total_rows":5000`)
	require.Contains(t, prompt, "did not inspect the full CSV")
	require.Contains(t, prompt, "does not establish completeness of the underlying business population")
	require.Less(t, len(prompt), maxParsedPromptBytes+2_000)

	_, err = describer.Describe(context.Background(), LocalFile{
		Reference: Reference{ID: "csv", Kind: KindCSV, Name: "large.csv", MediaType: "text/csv"},
		Path:      path,
	}, "Try a second inspection goal.")
	require.ErrorIs(t, err, ErrInvalid)
	require.Equal(t, 1, model.calls)
}

func TestLLMDescriberRequiresAnalysisGoal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	require.NoError(t, writeTestFile(path, "# Note"))

	_, err := (&LLMDescriber{Model: &descriptionModel{response: "unused"}}).Describe(
		context.Background(),
		LocalFile{Reference: Reference{ID: "md", Kind: KindMarkdown, Name: "note.md"}, Path: path},
		"",
	)

	require.ErrorIs(t, err, ErrInvalid)
}

func TestLLMDescriberDoesNotFallbackToTextModelForImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screenshot.png")
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, png.Encode(file, image.NewRGBA(image.Rect(0, 0, 20, 20))))
	require.NoError(t, file.Close())
	textModel := &descriptionModel{response: "This must not be called."}

	description, err := (&LLMDescriber{Model: textModel}).Describe(
		context.Background(),
		LocalFile{
			Reference: Reference{ID: "image", Kind: KindImage, Name: "screenshot.png", MediaType: "image/png"},
			Path:      path,
		},
		"Identify the decision evidence.",
	)

	require.NoError(t, err)
	require.Contains(t, description, "no multimodal model")
	require.Zero(t, textModel.calls)
}

func TestCompactParsedArtifactBoundsLargeWorkbookPrompt(t *testing.T) {
	cell := strings.Repeat("x", 500)
	digest := Digest{
		Kind: KindSpreadsheet, Name: "large.xlsx",
		MediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Workbook:  &WorkbookDigest{},
	}
	for sheetIndex := 0; sheetIndex < 100; sheetIndex++ {
		sheet := SheetDigest{
			Name: fmt.Sprintf("Sheet %d", sheetIndex), RowCount: 100_000, ColumnCount: 100,
		}
		for rowIndex := 0; rowIndex < MaxSampleRows; rowIndex++ {
			row := make([]string, MaxColumns)
			for columnIndex := range row {
				row[columnIndex] = cell
			}
			sheet.SampleRows = append(sheet.SampleRows, row)
		}
		digest.Workbook.Sheets = append(digest.Workbook.Sheets, sheet)
	}

	encoded, err := compactParsedArtifact(digest)

	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), maxParsedPromptBytes)
	require.Contains(t, encoded, "Additional worksheets were omitted")
	require.NotContains(t, encoded, "Sheet 99")
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

type descriptionModel struct {
	mu       sync.Mutex
	response string
	calls    int
	inputs   [][]*schema.Message
}

func (m *descriptionModel) Generate(
	_ context.Context,
	input []*schema.Message,
	_ ...einomodel.Option,
) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.inputs = append(m.inputs, input)
	return schema.AssistantMessage(m.response, nil), nil
}

func (m *descriptionModel) Stream(
	ctx context.Context,
	input []*schema.Message,
	options ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *descriptionModel) WithTools(
	[]*schema.ToolInfo,
) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}
