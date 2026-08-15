package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientAcceptsResponseAtByteLimit(t *testing.T) {
	payload := responsePayloadAtLimit(t)

	response, _, err := testClient(t, "token-at-limit", http.StatusOK, payload).request(
		context.Background(), http.MethodGet, "/resource", nil, 0, "", false,
	)

	require.NoError(t, err)
	require.Equal(t, payload, string(response))
}

func TestClientRejectsResponseOneByteOverLimitWithoutPartialJSON(t *testing.T) {
	const token = "token-one-byte-over"
	payload := responsePayloadAtLimit(t) + "\n"
	require.Len(t, payload, maxResponseBodyBytes+1)

	response, _, err := testClient(t, token, http.StatusOK, payload).request(
		context.Background(), http.MethodGet, "/resource", nil, 0, "", false,
	)

	require.Nil(t, response)
	var apiError *APIError
	require.ErrorAs(t, err, &apiError)
	require.Equal(t, "RESPONSE_TOO_LARGE", apiError.Code)
	require.NotContains(t, fmt.Sprintf("%+v", apiError), token)
	require.NotContains(t, fmt.Sprintf("%+v", apiError), payload[:64])
}

func TestClientResponseErrorsDoNotExposeCredentialsOrBodies(t *testing.T) {
	const token = "token-must-remain-secret"
	for _, testCase := range []struct {
		name         string
		body         string
		expectedCode string
	}{
		{
			name:         "oversized_error_response",
			body:         strings.Repeat("sensitive response body ", maxResponseBodyBytes/20+1),
			expectedCode: "RESPONSE_TOO_LARGE",
		},
		{
			name:         "malformed_error_response",
			body:         "malformed sensitive response body " + token,
			expectedCode: "HTTP_ERROR",
		},
		{
			name:         "schema_malformed_error_response",
			body:         `{"code":"` + token + `","detail":"sensitive response body","title":false}`,
			expectedCode: "HTTP_ERROR",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response, _, err := testClient(t, token, http.StatusBadGateway, testCase.body).request(
				context.Background(), http.MethodGet, "/resource", nil, 0, "", false,
			)

			require.Nil(t, response)
			var apiError *APIError
			require.ErrorAs(t, err, &apiError)
			require.Equal(t, testCase.expectedCode, apiError.Code)
			diagnostic := fmt.Sprintf("%+v", apiError)
			require.NotContains(t, diagnostic, token)
			require.NotContains(t, diagnostic, "sensitive response body")
		})
	}
}

func responsePayloadAtLimit(t *testing.T) string {
	t.Helper()
	const envelopeBytes = len(`{"value":""}`)
	payload := `{"value":"` + strings.Repeat("x", maxResponseBodyBytes-envelopeBytes) + `"}`
	require.Len(t, payload, maxResponseBodyBytes)
	return payload
}

func testClient(t *testing.T, token string, status int, body string) *client {
	t.Helper()
	return &client{
		server: "https://pactline.invalid",
		token:  token,
		httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
			return &http.Response{
				StatusCode: status,
				Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		})},
		verbose: func(string, ...any) {},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
