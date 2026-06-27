// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license elegance.

package transport

import (
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni"
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

func TestRequest_FluentConfiguration_SavesExpectedParameters(t *testing.T) {
	t.Parallel()

	target := mockTarget{name: "dest"}
	req := NewRequest(target, strings.NewReader("body"))

	req.WithParam("a", "1").
		WithParams(url.Values{"b": {"2"}, "c": {"3"}}).
		WithHeader("X-Test", "true").
		WithParam("access_token", "secret").
		WithRoutingAppID(440).
		WithForceProto()

	body, _ := io.ReadAll(req.Body)
	assert.Equal(t, "body", string(body))
	assert.Equal(t, target, req.Target())
	assert.Equal(t, "1", req.Params().Get("a"))
	assert.Equal(t, "2", req.Params().Get("b"))
	assert.Equal(t, "3", req.Params().Get("c"))
	assert.Equal(t, "true", req.Header().Get("X-Test"))
	assert.Equal(t, "secret", req.Token())
	assert.Equal(t, uint32(440), req.RoutingAppID())
	assert.True(t, req.IsForceProto())
}

func TestRequest_DecoderAndModifiers_SavesAndRetrieves(t *testing.T) {
	t.Parallel()

	req := NewRequest(mockTarget{name: "dest"}, nil)

	var dummyDecoder aoni.Decoder
	assert.Nil(t, req.Decoder(dummyDecoder))

	req.SetDecoder(dummyDecoder)
	assert.Equal(t, dummyDecoder, req.Decoder(nil))

	var dummyMod aoni.RequestModifier
	req.WithModifier(dummyMod)
	assert.Len(t, req.Modifiers(), 1)
}

func TestResponse_AsAndHelpers_ExtractsExpectedMetadata(t *testing.T) {
	t.Parallel()

	t.Run("as_success", func(t *testing.T) {
		t.Parallel()

		type myMeta struct{ ID int }

		resp := NewResponse(nil, myMeta{ID: 42})

		var extracted myMeta

		ok := resp.As(&extracted)
		assert.True(t, ok)
		assert.Equal(t, 42, extracted.ID)
	})

	t.Run("as_failure_type_mismatch", func(t *testing.T) {
		t.Parallel()

		resp := NewResponse(nil, HTTPMetadata{StatusCode: 200})

		var wrongType string

		ok := resp.As(&wrongType)
		assert.False(t, ok)
	})

	t.Run("as_failure_nil_metadata", func(t *testing.T) {
		t.Parallel()

		resp := NewResponse(nil, nil)

		var m HTTPMetadata
		assert.False(t, resp.As(&m))
	})

	t.Run("as_panic_not_a_pointer", func(t *testing.T) {
		t.Parallel()

		resp := NewResponse(nil, 1)
		assert.Panics(t, func() {
			resp.As(123)
		})
	})

	t.Run("helper_coverage", func(t *testing.T) {
		t.Parallel()

		hMeta := HTTPMetadata{StatusCode: 404}
		respH := NewResponse(nil, hMeta)
		gotH, okH := respH.HTTP()
		assert.True(t, okH)
		assert.Equal(t, 404, gotH.StatusCode)

		sMeta := SocketMetadata{SourceJobID: 123}
		respS := NewResponse(nil, sMeta)
		gotS, okS := respS.Socket()
		assert.True(t, okS)
		assert.Equal(t, uint64(123), gotS.SourceJobID)

		_, ok := respH.Socket()
		assert.False(t, ok)
		_, ok = respS.HTTP()
		assert.False(t, ok)
	})
}
