package transport

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

const HTTPUserAgent = "Valve/Steam HTTP Client 1.0"

type HTTPTransport struct {
	client *rest.Client
}

type HTTPTarget interface {
	Target
	HTTPPath() string
	HTTPMethod() string
}

var _ Transport = (*HTTPTransport)(nil)

func NewHTTPTransport(doer rest.HTTPDoer, baseURL string) *HTTPTransport {
	c := rest.NewClient(doer, baseURL)
	c.SetHeader("User-Agent", HTTPUserAgent)
	return &HTTPTransport{client: c}
}

func (t *HTTPTransport) Do(req *Request) (*Response, error) {
	target, ok := req.Target().(HTTPTarget)
	if !ok {
		return nil, fmt.Errorf("http: target %T does not support HTTP transport", req.Target())
	}

	v := req.Params()

	if len(req.Body()) > 0 {
		v.Set("input_protobuf_encoded", base64.StdEncoding.EncodeToString(req.Body()))
	}

	mods := []rest.RequestModifier{
		func(r *http.Request) {
			for key, values := range req.Header() {
				for _, val := range values {
					r.Header.Add(key, val)
				}
			}
			r.Header.Set("Accept", "text/html,*/*;q=0.9")
		},
	}

	httpResp, err := t.client.Request(req.Context(), target.HTTPMethod(), target.HTTPPath(), nil, v, mods...)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("http: failed to read response: %w", err)
	}

	return &Response{
		Header:     httpResp.Header,
		StatusCode: httpResp.StatusCode,
		Result:     t.parseEResult(httpResp),
		Body:       body,
	}, nil
}

func (t *HTTPTransport) parseEResult(resp *http.Response) protocol.EResult {
	if resHeader := resp.Header.Get("x-eresult"); resHeader != "" {
		if val, err := strconv.Atoi(resHeader); err == nil {
			return protocol.EResult(val)
		}
	}
	return protocol.EResult_OK
}

func (t *HTTPTransport) Close() error {
	return nil
}
