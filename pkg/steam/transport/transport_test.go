// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

type mockTarget struct {
	name string
}

func (m mockTarget) String() string { return m.name }

type mockEHeader struct {
	result    enums.EResult
	sourceJob uint64
}

func (m mockEHeader) GetEResult() enums.EResult     { return m.result }
func (m mockEHeader) GetSourceJob() uint64          { return m.sourceJob }
func (m mockEHeader) GetTargetJob() uint64          { return 0 }
func (m mockEHeader) SerializeTo(w io.Writer) error { return nil }

func TestRequest_FluentAPI(t *testing.T) {
	target := mockTarget{name: "dest"}
	req := NewRequest(target, strings.NewReader("body"))

	req.WithParam("a", "1").
		WithParams(url.Values{"b": {"2"}, "c": {"3"}}).
		WithHeader("X-Test", "true").
		WithParam("access_token", "secret")

	body, _ := io.ReadAll(req.Body)
	assert.Equal(t, "body", string(body))
	assert.Equal(t, target, req.Target())
	assert.Equal(t, "1", req.Params().Get("a"))
	assert.Equal(t, "2", req.Params().Get("b"))
	assert.Equal(t, "3", req.Params().Get("c"))
	assert.Equal(t, "true", req.Header().Get("X-Test"))
	assert.Equal(t, "secret", req.Token())
}

func TestResponse_MetadataExtraction(t *testing.T) {
	t.Run("As Success", func(t *testing.T) {
		type myMeta struct{ ID int }

		resp := NewResponse(nil, myMeta{ID: 42})

		var extracted myMeta

		ok := resp.As(&extracted)
		assert.True(t, ok)
		assert.Equal(t, 42, extracted.ID)
	})

	t.Run("As Failure - Type Mismatch", func(t *testing.T) {
		resp := NewResponse(nil, HTTPMetadata{StatusCode: 200})

		var wrongType string

		ok := resp.As(&wrongType)
		assert.False(t, ok)
	})

	t.Run("As Failure - Nil Metadata", func(t *testing.T) {
		resp := NewResponse(nil, nil)

		var m HTTPMetadata
		assert.False(t, resp.As(&m))
	})

	t.Run("As Panic - Not a Pointer", func(t *testing.T) {
		resp := NewResponse(nil, 1)
		assert.Panics(t, func() {
			resp.As(123)
		})
	})

	t.Run("Helper Coverage", func(t *testing.T) {
		// HTTP helper
		hMeta := HTTPMetadata{StatusCode: 404}
		respH := NewResponse(nil, hMeta)
		gotH, okH := respH.HTTP()
		assert.True(t, okH)
		assert.Equal(t, 404, gotH.StatusCode)

		// Socket helper
		sMeta := SocketMetadata{SourceJobID: 123}
		respS := NewResponse(nil, sMeta)
		gotS, okS := respS.Socket()
		assert.True(t, okS)
		assert.Equal(t, uint64(123), gotS.SourceJobID)

		// Negative checks for helpers
		_, ok := respH.Socket()
		assert.False(t, ok)
		_, ok = respS.HTTP()
		assert.False(t, ok)
	})
}
