package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/inventory/provider"
)

var (
	ErrPrivateProfile = errors.New("inventory: profile is private")
	ErrFakeRedirect   = errors.New("inventory: received fake redirect 0 from proxy")
	ErrMalformed      = errors.New("inventory: malformed response")

	rxErrorEResult = regexp.MustCompile(`^(.+) \((\d+)\)$`)
)

type Client struct {
	httpDoer rest.HTTPDoer
}

func NewClient(doer rest.HTTPDoer) *Client {
	if doer == nil {
		doer = http.DefaultClient
	}
	return &Client{httpDoer: doer}
}

// FetchInventory downloads the user's inventory through the specified provider.
func (c *Client) FetchInventory(
	ctx context.Context,
	provider provider.Provider,
	steamID uint64,
	appID uint32,
	contextID uint64,
	tradableOnly bool,
) (inventory []EconItem, currency []EconItem, err error) {
	targetURL, baseQuery, headers := provider.RequestInfo(steamID, appID, contextID)
	if baseQuery == nil {
		baseQuery = url.Values{}
	}

	startAssetID := ""
	pos := 1

	for {
		reqURL := targetURL
		q := url.Values{}
		maps.Copy(q, baseQuery)

		if startAssetID != "" {
			q.Set("start_assetid", startAssetID)
		}

		if len(q) > 0 {
			reqURL = fmt.Sprintf("%s?%s", targetURL, q.Encode())
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		if headers != nil {
			req.Header = headers
		}

		resp, err := c.httpDoer.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("http request failed: %w", err)
		}

		var result Response
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if err != nil {
			if resp.StatusCode == http.StatusForbidden {
				return nil, nil, ErrPrivateProfile
			}
			return nil, nil, fmt.Errorf("failed to decode json (status %d): %w", resp.StatusCode, err)
		}

		if resp.StatusCode == http.StatusInternalServerError && result.Error != "" {
			return nil, nil, parseSteamError(result.Error)
		}

		if result.FakeRedirect != nil && *result.FakeRedirect == 0 {
			return nil, nil, ErrFakeRedirect
		}

		if result.Success != 1 {
			errMsg := result.Error
			if errMsg == "" {
				errMsg = result.ErrorCapital
			}
			if errMsg == "" {
				errMsg = "unknown proxy error"
			}
			return nil, nil, fmt.Errorf("proxy returned error: %s", errMsg)
		}

		if result.TotalInventoryCount == 0 {
			break
		}

		if result.Assets == nil || result.Descriptions == nil {
			return nil, nil, ErrMalformed
		}

		descMap := make(map[string]Description, len(result.Descriptions))
		for _, d := range result.Descriptions {
			key := GetDescriptionKey(d.ClassID, d.InstanceID)
			descMap[key] = d
		}

		for _, asset := range result.Assets {
			key := GetDescriptionKey(asset.ClassID, asset.InstanceID)
			desc, ok := descMap[key]
			if !ok {
				continue
			}

			if tradableOnly && desc.Tradable == 0 {
				continue
			}

			item := EconItem{
				Asset:       asset,
				Description: desc,
				Pos:         pos,
				ContextID:   contextID,
			}
			pos++

			if asset.CurrencyID != "" {
				currency = append(currency, item)
			} else {
				inventory = append(inventory, item)
			}
		}

		if result.MoreItems == 1 {
			startAssetID = result.LastAssetID
		} else {
			break
		}
	}

	return inventory, currency, nil
}

func parseSteamError(errText string) error {
	matches := rxErrorEResult.FindStringSubmatch(errText)
	if len(matches) == 3 {
		eresult, _ := strconv.Atoi(matches[2])
		return fmt.Errorf("steam error: %s (EResult: %d)", matches[1], eresult)
	}
	return errors.New(errText)
}
