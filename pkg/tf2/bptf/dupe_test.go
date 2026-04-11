// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	inv "github.com/lemon4ksan/g-man/pkg/tf2/inventory"
)

type mockRoundTripper struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

type MockDupeChecker struct {
	Responses map[uint64]inv.HistoryStatus
	Err       error
}

func (m *MockDupeChecker) CheckHistory(ctx context.Context, id uint64) (inv.HistoryStatus, error) {
	if m.Err != nil {
		return inv.HistoryStatus{}, m.Err
	}

	return m.Responses[id], nil
}

func createBptfHTML(hasTable, isDuped bool) string {
	var sb strings.Builder
	sb.WriteString("<html><body>")

	if hasTable {
		sb.WriteString("<table><tr><td>History</td></tr></table>")

		if isDuped {
			sb.WriteString(`<button id="dupe-modal-btn">Duplicated</button>`)
		}
	}

	sb.WriteString("</body></html>")

	return sb.String()
}

func TestBackpackTFChecker_CheckHistory(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantStatus inv.HistoryStatus
		wantErr    bool
	}{
		{
			name:       "404 Not Found",
			statusCode: 404,
			wantStatus: inv.HistoryStatus{Recorded: false},
			wantErr:    true,
		},
		{
			name:       "200 OK, Clean",
			statusCode: 200,
			body:       createBptfHTML(true, false),
			wantStatus: inv.HistoryStatus{Recorded: true, IsDuped: false},
		},
		{
			name:       "200 OK, Duped",
			statusCode: 200,
			body:       createBptfHTML(true, true),
			wantStatus: inv.HistoryStatus{Recorded: true, IsDuped: true},
		},
		{
			name:       "200 OK, No Table (Not recorded)",
			statusCode: 200,
			body:       createBptfHTML(false, false),
			wantStatus: inv.HistoryStatus{Recorded: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := &http.Client{
				Transport: &mockRoundTripper{
					fn: func(req *http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: tt.statusCode,
							Body:       io.NopCloser(strings.NewReader(tt.body)),
						}, nil
					},
				},
			}

			checker := &BackpackTFChecker{
				bptfClient: New(httpClient, "", ""),
			}

			got, err := checker.CheckHistory(context.Background(), 123)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckHistory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(got, tt.wantStatus) {
				t.Errorf("CheckHistory() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}
