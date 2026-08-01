package domain

import "testing"

func TestValidateAttachmentMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		filename  string
		mediaType string
		size      int64
		wantError bool
	}{
		{name: "image", filename: "result.png", mediaType: "image/png", size: 42},
		{name: "maximum", filename: "report.csv", mediaType: "text/csv", size: MaxAttachmentBytes},
		{name: "path", filename: "../secret.md", mediaType: "text/markdown", size: 1, wantError: true},
		{name: "executable", filename: "payload.exe", mediaType: "application/octet-stream", size: 1, wantError: true},
		{name: "script media type", filename: "payload.txt", mediaType: "application/x-sh", size: 1, wantError: true},
		{name: "too large", filename: "large.csv", mediaType: "text/csv", size: MaxAttachmentBytes + 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateAttachmentMetadata(test.filename, test.mediaType, test.size)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateAttachmentMetadata() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestAttachmentPreview(t *testing.T) {
	t.Parallel()
	if got := AttachmentPreview("prototype.html", "application/octet-stream"); got != AttachmentPreviewHTML {
		t.Fatalf("HTML preview = %q", got)
	}
	if got := AttachmentPreview("data.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"); got != AttachmentPreviewDownload {
		t.Fatalf("spreadsheet preview = %q", got)
	}
	if got := AttachmentPreview("vector.svg", "image/svg+xml"); got != AttachmentPreviewDownload {
		t.Fatalf("SVG preview = %q", got)
	}
}

func TestValidateAttachmentContentSignatureRejectsDisguisedExecutables(t *testing.T) {
	t.Parallel()
	for _, header := range [][]byte{
		[]byte("MZ disguised executable"),
		{0x7f, 'E', 'L', 'F'},
		[]byte("#!/bin/sh"),
	} {
		if err := ValidateAttachmentContentSignature(header); err == nil {
			t.Fatalf("expected dangerous content %x to be rejected", header)
		}
	}
}
