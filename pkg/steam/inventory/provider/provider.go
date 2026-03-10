package provider

import (
	"fmt"
	"net/http"
	"net/url"
)

// Provider describes a contract for a third-party inventory provider.
type Provider interface {
	Name() string
	// RequestInfo returns the URL, query parameters, and headers for the request.
	RequestInfo(steamID uint64, appID uint32, contextID uint64) (targetURL string, query url.Values, headers http.Header)
}

type SteamSupply struct {
	APIKey string
}

func (p SteamSupply) Name() string { return "SteamSupply" }

func (p SteamSupply) RequestInfo(steamID uint64, appID uint32, contextID uint64) (string, url.Values, http.Header) {
	target := fmt.Sprintf("https://steam.supply/API/%s/loadinventory", p.APIKey)
	q := url.Values{}
	q.Set("steamid", fmt.Sprint(steamID))
	q.Set("appid", fmt.Sprint(appID))
	q.Set("contextid", fmt.Sprint(contextID))

	return target, q, nil
}

type SteamApis struct {
	APIKey string
}

func (p SteamApis) Name() string { return "SteamApis" }

func (p SteamApis) RequestInfo(steamID uint64, appID uint32, contextID uint64) (string, url.Values, http.Header) {
	target := fmt.Sprintf("https://api.steamapis.com/steam/inventory/%d/%d/%d", steamID, appID, contextID)
	q := url.Values{}
	q.Set("api_key", p.APIKey)

	return target, q, nil
}

type ExpressLoad struct {
	APIKey string
}

func (p ExpressLoad) Name() string { return "ExpressLoad" }

func (p ExpressLoad) RequestInfo(steamID uint64, appID uint32, contextID uint64) (string, url.Values, http.Header) {
	target := fmt.Sprintf("https://api.express-load.com/inventory/%d/%d/%d", steamID, appID, contextID)

	headers := make(http.Header)
	headers.Set("X-API-KEY", p.APIKey)
	headers.Set("User-Agent", "g-man client")

	return target, nil, headers
}
