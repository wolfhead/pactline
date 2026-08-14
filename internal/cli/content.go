package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func content(message, path string, stdin io.Reader, label string) (string, error) {
	if (message == "") == (path == "") {
		return "", &APIError{Code: "USAGE", Message: fmt.Sprintf("provide exactly one --%s or --%s-file", label, label)}
	}
	if path == "" {
		if strings.TrimSpace(message) == "" {
			return "", &APIError{Code: "USAGE", Message: label + " cannot be empty"}
		}
		return message, nil
	}
	var body []byte
	var err error
	if path == "-" {
		body, err = io.ReadAll(io.LimitReader(stdin, 1024*1024))
	} else {
		body, err = os.ReadFile(path)
	}
	if err != nil {
		return "", &APIError{Code: "USAGE", Message: "read " + label + ": " + err.Error()}
	}
	value := string(body)
	if strings.TrimSpace(value) == "" {
		return "", &APIError{Code: "USAGE", Message: label + " cannot be empty"}
	}
	return value, nil
}
