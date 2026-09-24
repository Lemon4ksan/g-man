# G-MAN Steam SDK Parity & Compliance

This document maps `g-man` packages, data structures, and runtime behaviors against standard Node.js libraries (`steam-user`, `@tf2autobot/steamcommunity`, `@tf2autobot/tradeoffer-manager`, `steam-totp`).

## 1. Component Mapping Matrix

| Domain | Node.js Reference | `g-man` Implementation (Go) | Parity Status | Architectural Improvement / Rationale |
| :--- | :--- | :--- | :--- | :--- |
| **Logon & CM Socket** | `steam-user/components/09-logon.js` | `pkg/steam/auth`, `pkg/steam/client` | Full parity | Fast socket reconnect, heartbeat monitoring, OAuth2 token authentication flow |
| **Web Sessions & Cookies** | `@tf2autobot/steamcommunity` | `pkg/steam/auth/websession` | Extended | TokenRefresher via CM socket (`GenerateAccessTokenForApp`), cookie sync across 5 domains, singleflight ReAuth |
| **2FA Code Generation** | `steam-totp` | `pkg/steam/guard`, `internal/crypto` | Full parity | RFC 6238 TOTP, custom base-32 alphabet, automatic Steam server time offset sync |
| **Mobile Confirmations** | `@tf2autobot/steamcommunity` (`confirmations.js`) | `pkg/steam/guard` (`service.go`, `guard.go`) | Extended | Tag parity (`allow`/`cancel` and `accept`/`reject`), multiajaxop `needauth`/`detail` parsing, empty response nil-safety, `%w` error typing |
| **Avatar Upload** | `steamcommunity/components/profile.js` | `pkg/steam/community/profile` (`UploadAvatar`) | Full parity | Multipart `FileUploader` payload with binary image MIME detection |
| **Profile & Privacy** | `steamcommunity/components/profile.js` | `pkg/steam/community/profile` (`EditProfile`, `UpdatePrivacySettings`) | Full parity | `ajaxsetprivacy`, profile metadata and `edit/info` scraping |
| **Persona & Playing Games** | `steam-user/components/friends.js` | `pkg/steam/social/status`, `pkg/steam/social/friends` | Extended | Multi-AppID, Rich Status Marquee, high-priority temporary trade status flash (`FlashStatus`) |
| **Partner SteamID Normalization** | `@tf2autobot/tradeoffer-manager` (`TradeOffer.js:13-21`) | `pkg/trading/web/actions.go` (`NormalizePartnerSteamID`) | Full parity | Validates 64-bit individual SteamID; converts 32-bit AccountIDs; fails fast on 0 to eliminate HTTP 403 Forbidden |
| **Send Offer Payload Parity** | `@tf2autobot/tradeoffer-manager` (`TradeOffer.js:391-400`) | `pkg/trading/web/models.go` (`sendNewReq`), `api.go` | Full parity | Enforces mandatory `serverid=1`, `partner` (64-bit SteamID), and `captcha=""` form fields; zero-allocation buffer pooling |
| **Accept State Fallback Recovery** | `@tf2autobot/tradeoffer-manager` (`TradeOffer.js:610-638`) | `pkg/trading/web/actions.go` (`AcceptOfferWithPartner`) | Extended | Inspects WebAPI state on HTTP 5xx/timeout: recovers State 3 (`Accepted`), State 11 (`InEscrow`), and State 9 (`CreatedNeedsConfirmation` -> emits `ConfirmationRequiredEvent`) |
| **Decline & Cancel Operations** | `@tf2autobot/tradeoffer-manager` (`TradeOffer.js:481-502`) | `pkg/trading/web/actions.go` (`DeclineOffer`, `CancelOffer`) | Extended | Dual-tier resilience: attempts WebAPI first, automatically falling back to Steam Community web endpoints on error |
| **Escrow Hold Parsing** | `@tf2autobot/tradeoffer-manager` (`TradeOffer.js:806-877`) | `pkg/trading/web/escrow.go` (`parseEscrowFromHTML`, `CheckEscrow`) | Extended | Substring extraction handles lowercase `g_daysTheirEscrow` fast-path with regex fallback; supports outgoing trade holds via pre-trade URL |
| **Glitched Offer Detection** | `@tf2autobot/tradeoffer-manager` (`TradeOffer.js:72-82`) | `pkg/trading/offer.go` (`IsGlitched`) | Full parity | Flags offers with 0 items regardless of message, missing partner SteamID, or missing item `Name` / `MarketHashName` |
| **Polling Watermark Freezing** | `@tf2autobot/tradeoffer-manager` (`polling.js:258-268`) | `pkg/trading/web/poller.go` (`doPoll`) | Full parity | Freezes `m.offersSince` timestamp watermark when any offer in the batch is glitched, preventing offer stranding |
| **Retriable Error Classification** | `@tf2autobot/tradeoffer-manager`, `@tf2autobot/tf2` | `pkg/steam/service/errors.go` (`IsRetriable`) | Extended | Classifies transient EResults (16, 20, 28, 10, 29) and HTTP 429/5xx status codes into typed retriable errors |
| **CM Socket Heartbeat & Threshold Reconnect** | `steam-user/components/09-logon.js` (`_heartbeat`) | `pkg/steam/socket/socket.go` (`StartHeartbeat`), `config.go` (`DefaultMaxHeartbeatFailures`) | Extended Parity | Proactive transmission at 2/3 interval with bounded send timeout (`min(sendInterval, 5s)`). Consecutive send failures reaching `MaxHeartbeatFailures` (default 3) automatically close transport and trigger clean reconnect, preventing hung sockets. |
| **CM Reconnect Double-Connect Elimination** | `steam-user/components/09-logon.js` | `pkg/steam/auth/auth.go` (`LogOn`, `LogOnAnonymous`), `internal/client/session/session.go` (`LogonServer`, `SetLogonServer`) | Full Parity | Checks `IsConnecting()` and `IsConnected()` on `SocketProvider`; awaits in-progress dial via `WaitForConnection(ctx)`. Reuses existing connection and updates active server via `CurrentServer()`, eliminating duplicate socket allocation during reconnect. |

## 2. Authentication & Session Management

### 2.1 WebSession & Cookie Synchronization
In Node.js, `steamcommunity` requires manual cookie injection after `steam-user` emits `webSession`. If cookies expire, requests fail with 401/403 until the application explicitly re-authenticates.

In `g-man`:
- `websession.WebSession` manages an in-memory `http.CookieJar` synchronized across 5 domains:
  - `https://steamcommunity.com`
  - `https://store.steampowered.com`
  - `https://help.steampowered.com`
  - `https://login.steampowered.com`
  - `https://s.team`
- **Fast-Path Token Refresher:** When platform credentials (`SteamClient` or `MobileApp`) require renewal, `WebSession.Authenticate` invokes `WithTokenRefresher` to request access tokens directly via CM socket `GenerateAccessTokenForApp` without calling blocked `/jwt/finalizelogin` endpoints.
- **ReAuth Middleware:** `REST()` wraps HTTP requests in a singleflight re-authentication middleware. If Steam returns 401 or 403, active requests are paused, `Refresh(ctx)` is triggered, cookies are updated, and failed requests retry automatically.
- **Auto-Refresh Loop:** Proactively refreshes web cookies before Steam session invalidation occurs.

### 2.2 2FA TOTP & Steam Guard Confirmations
- `internal/crypto/totp.go`: Implements Steam's custom alphanumeric base-32 alphabet (`23456789BCDFGHJKMNPQRTVWXY`) over HMAC-SHA1.
- `pkg/steam/guard/guard.go` & `service.go`:
  - Decodes identity and shared secrets from Base64 or Hex strings.
  - Automatically fetches and applies Steam Server Time Offset via `IAuthenticationService/GetPasswordRSAPublicKey`.
  - Generates confirmation keys (`ck`) supporting both standard and alias action tags: `allow` (alias for `accept`) and `cancel` (alias for `reject`), guaranteeing compatibility with `@tf2autobot/steamcommunity` and official Valve endpoints.
  - In `mobileconf/multiajaxop`, parses `detail` and `needauth` fields, mapping authentication expiry to `service.ErrSessionExpired`.
  - Implements defensive nil response checks, preventing nil-pointer panics on empty HTTP 200 bodies, and wraps errors with `%w: %s` via `ErrConfirmationRejected`.

## 3. Profile, Avatar, and Persona Presence

### 3.1 Avatar Upload
`profile.UploadAvatar` posts a multipart form to `https://steamcommunity.com/actions/FileUploader`:
- Form fields:
  - `type`: `player_avatar_image`
  - `sId`: SteamID64
  - `sessionid`: Active session ID from cookie jar
  - `doSub`: `1`
  - `json`: `1`
  - `avatar`: Binary image buffer with detected MIME type (`image/png`, `image/jpeg`, `image/gif`) and filename.
- Returns the avatar SHA-1 hash to construct static CDN URLs: `https://avatars.steamstatic.com/{hash}_full.jpg`.

### 3.2 Persona State & Games Played
- **CM Status:** `friends.SetPersona(ctx, state, name)` sends `CMsgClientChangeStatus` (EMsg 716) over the active CM socket.
- **Multi-Game Playing:** `status.Manager` sends `CMsgClientGamesPlayed` containing real AppIDs (e.g. 440 for TF2) along with a Non-Steam Game ID (`0x8000000000000000`) for custom status text.
- **Status Rotations:** Supports periodic cycling across configured text slides, marquee scrolling with fixed window size, and high-priority temporary status flashes (`FlashStatus`) on incoming trade events.

## 4. Web Trading Engine (`pkg/trading/web`)

### 4.1 Item Locking (`ReserveItems`)
To eliminate race conditions where the same asset could be offered in two simultaneous outgoing trades, `web.Manager.ReserveItems(assetIDs...)` locks IDs in a thread-safe mutex map.
- If an asset is already reserved, `SendOffer` fails immediately with `ErrItemAlreadyReserved`.
- Asset IDs are sorted before acquisition to avoid mutex deadlock.
- When `SendOffer` finishes (or fails), locks are released.

### 4.2 Partner SteamID64 Normalization & HTTP 403 Prevention
Steam Community endpoint `/tradeoffer/{offerID}/accept` strictly requires the 64-bit partner SteamID string in the form body (`partner`). Omitting `partner` or passing `"0"` causes Steam to reject the request with **HTTP 403 Forbidden**.
- `NormalizePartnerSteamID(partnerID id.ID) (id.ID, error)`:
  - Fails fast with `ErrInvalidPartnerSteamID` if `partnerID == 0`.
  - Automatically converts 32-bit AccountIDs (`< id.FromAccountID(0)`) to 64-bit individual SteamIDs via `id.FromAccountID(uint32(partnerID))`.
  - Validates `partnerID.IsValid()` and verifies account type `partnerID.Type() == id.AccountTypeIndividual`.
- In `AcceptOfferWithPartner`:
  - If `partnerID == 0`, queries `GetOffer` as an automatic discovery fallback. If lookup fails, returns `ErrInvalidPartnerSteamID` before any Community POST is attempted.
  - If partner ID is provided or discovered, validates and normalizes before dispatching form payload.
  - Steam Community accept endpoint is never called with `partner: "0"`.
- In `Processor.applyAction` and `Sidecar.handleTradeOffer`:
  - Directly passes `off.OtherSteamID` to `AcceptOfferWithPartner`, eliminating redundant `GetOffer` roundtrips.

### 4.3 Send Offer Form Payload Parity
Steam Community endpoint `/tradeoffer/new/send` mandates strict form parameters:
- `serverid=1`
- `partner`: 64-bit individual SteamID string
- `tradeoffermessage`: URL-escaped message string
- `json_tradeoffer`: JSON-serialized offer contents
- `captcha=""`: Empty captcha string parameter (mandatory even when no captcha challenge is active)
- `trade_offer_create_params`: JSON-serialized access token params (`{"trade_offer_access_token":"..."}`)
- Zero-allocation form serialization via per-P buffer pool (`formBufferPool`).

### 4.4 Error Recovery on `AcceptOffer` (Three-State Fallback)
Steam's `tradeoffer/{id}/accept` endpoint often returns HTTP 500 / 502 / 504 / 429 or network timeouts even when the trade was successfully processed on Steam's backend.
- When `community.PostFormTo[acceptResponse]` returns an HTTP error, `AcceptOfferWithPartner` queries `GetOffer(ctx, offerID)` via `IEconService/GetTradeOffer`.
- **State 3 (`OfferStateAccepted`):** The trade completed successfully on backend; returns `nil` (recovering from ambiguous network failure).
- **State 11 (`OfferStateInEscrow`):** The trade was accepted and placed into trade hold; returns `nil`.
- **State 9 (`OfferStateCreatedNeedsConfirmation`):** The trade was accepted on web but requires mobile 2FA confirmation; publishes `guard.ConfirmationRequiredEvent` to wake the confirmation poller and returns `nil`.
- Other states (e.g. State 2 Active, State 8 InvalidItems) return the original HTTP error.

### 4.5 Dual-Tier Decline & Cancel Resilience
Trade offers can be rejected through either WebAPI or Steam Community web endpoints:
- `DeclineOffer` and `CancelOffer` invoke WebAPI endpoints (`IEconService/DeclineTradeOffer/v1`, `CancelTradeOffer/v1`) first.
- If WebAPI fails (e.g. API key rate limiting or outage), operations automatically fall back to Community endpoints (`DeclineOfferCommunity`, `CancelOfferCommunity` via `tradeoffer/{id}/decline` and `cancel`).
- Fails fast on invalid offer ID (0) or unauthenticated session.

### 4.6 Escrow Duration Checking
`escrow.GetEscrowDuration` parses escrow holding days from trade offer HTML:
- **Case-Insensitive Fast-Path:** Evaluates lowercase `g_daysTheirEscrow = ` and `g_daysMyEscrow = ` before checking uppercase `g_DaysTheirEscrow = ` and falling back to regex.
- **Outgoing Trade Inspection:** Steam Community does not display escrow holding periods on `tradeoffer/{id}/` pages for outgoing sent trades. `CheckEscrow` queries the pre-trade URL `tradeoffer/new/?partner={partnerID.AccountID()}` to evaluate hold durations for outgoing trades.
- Fast-path checking honors `offer.State == OfferStateInEscrow` and `offer.EscrowEndDate > 0`.

### 4.7 Glitched Offer Detection (`IsGlitched`)
An offer is considered corrupted or incompletely loaded by Steam if:
1. `OtherSteamID == 0`.
2. Both `ItemsToGive` and `ItemsToReceive` are empty (regardless of whether a custom `Message` is present).
3. Any item has either `Name == ""` or `MarketHashName == ""` (matching `@tf2autobot/tradeoffer-manager` line 78: `!item.name || !item.market_hash_name`), indicating Steam inventory description replication lag.

### 4.8 Watermark Freezing on Glitched Offers
During `Manager.doPoll`:
- The poller inspects all offers in the poll batch for `off.IsGlitched()`.
- If `hasGlitchedOffer == true`:
  - `m.offersSince` timestamp watermark is **frozen** at its current value.
  - Prevents advancing the cutoff timestamp, ensuring Steam's next `GetTradeOffers` poll cycle re-queries the glitched offer once Steam description caches finish populating.
- When all offers in the batch are valid, `m.offersSince` advances to `max(TimeUpdated)` across the batch.

## 5. Client Retry & Error Resilience (`cmd/g-mand`, `pkg/tf2/driver`)

- **Retriable Error Hierarchy:** `isRetriableError` in daemon and driver evaluates `service.IsRetriable(err)`:
  - Retries on transient Steam errors: `EResult_Timeout` (16), `EResult_ServiceUnavailable` (20), `EResult_LimitExceeded` (28), `EResult_Busy` (10), `EResult_TryAnotherCM` (29).
  - Retries on HTTP 429 (Rate Limit) and HTTP 5xx (Internal Server Error, Bad Gateway, Service Unavailable, Gateway Timeout).
  - Fails fast without retry on terminal errors: HTTP 403 Forbidden, `EResult_AccessDenied` (15), `EResult_InvalidState` (8).

## 6. Steam CM Socket Heartbeat & Transport Resilience (`pkg/steam/socket`)

### 6.1 Proactive Heartbeat Cadence
In standard Node.js implementations (`steam-user`), heartbeats are sent at the exact negotiated interval, risking timeouts if packets experience transport jitter.
- `Socket.StartHeartbeat` calculates `sendInterval := interval * 2 / 3` (e.g., ~6.6 seconds for a 10-second Steam CM interval).
- Clamps non-positive intervals to `DefaultHeartbeatInterval` (10s) to prevent `time.NewTicker` panics.
- Heartbeat send operations are bounded by `sendTimeout := min(sendInterval, 5*time.Second)`, preventing socket send stalls from blocking the heartbeat scheduler.

### 6.2 Consecutive Failure Threshold & Automated Reconnection Trigger
Steam Connection Managers occasionally silently hang without terminating TCP sockets.
- `Socket` tracks `consecutiveFailures`.
- Each successful `CMsgClientHeartBeat` transmission resets `consecutiveFailures = 0`.
- If `consecutiveFailures >= cfg.MaxHeartbeatFailures` (default 3), the socket logs a critical event, closes transport via `s.conn.Disconnect()`, and initiates automatic CM reconnect via `s.conn.TriggerReconnect()`.

## 7. Double-Connect Elimination & Connection State Synchronization (`pkg/steam/auth`, `internal/client/session`)

### 7.1 Connection Dial Deduplication
When background reconnect loops trigger CM reconnection:
- `auth.LogOn` and `auth.LogOnAnonymous` inspect `connChecker.IsConnecting()`.
- If dialing is in progress, callers await completion via `waiter.WaitForConnection(loginCtx)` rather than spawning redundant TCP connections.
- If `socket.IsConnected()` reports true, dialing is skipped completely, preventing socket collisions.

### 7.2 Active CM Server State Synchronization
- `session.Session` tracks `logonServer socket.CMServer` via thread-safe `SetLogonServer` and `LogonServer()`.
- Upon successful reconnect, `sock.SetOnReconnect` invokes `session.Reconnect()`, synchronizing `session.logonServer` with `socket.CurrentServer()`.

