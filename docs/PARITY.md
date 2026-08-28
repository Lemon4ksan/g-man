# G-MAN Steam SDK Parity & Compliance

This document maps `g-man` packages, data structures, and runtime behaviors against standard Node.js libraries (`steam-user`, `@tf2autobot/steamcommunity`, `@tf2autobot/tradeoffer-manager`, `steam-totp`).

## 1. Component Mapping Matrix

| Domain | Node.js Reference | `g-man` Implementation (Go) | Parity Status |
| :--- | :--- | :--- | :--- |
| **Logon & CM Socket** | `steam-user/components/09-logon.js` | `pkg/steam/auth`, `pkg/steam/client` | Full parity (OAuth2 token flow, CM routing) |
| **Web Sessions & Cookies** | `@tf2autobot/steamcommunity` | `pkg/steam/auth/websession` | Extended (Auto-refresh every 6h, ReAuth middleware) |
| **2FA Code Generation** | `steam-totp` | `pkg/steam/guard`, `internal/crypto` | Full parity (RFC 6238 TOTP, Steam time offset) |
| **Mobile Confirmations** | `steamcommunity/components/confirmations.js` | `pkg/steam/guard` (`GetConfirmations`, `RespondToConfirmation`, `RespondToMultiple`) | Full parity (HMAC-SHA1 keys, batch actions) |
| **Avatar Upload** | `steamcommunity/components/profile.js` | `pkg/steam/community/profile` (`UploadAvatar`) | Full parity (Multipart `FileUploader` payload) |
| **Profile & Privacy** | `steamcommunity/components/profile.js` | `pkg/steam/community/profile` (`EditProfile`, `UpdatePrivacySettings`) | Full parity (`ajaxsetprivacy`, `edit/info` scraping) |
| **Persona & Playing Games** | `steam-user/components/friends.js` | `pkg/steam/social/status`, `pkg/steam/social/friends` | Extended (Multi-AppID, Rich Status Marquee, Trade Flash) |
| **Trade Offer Lifecycle** | `@tf2autobot/tradeoffer-manager` | `pkg/trading/web` (`actions.go`, `poller.go`, `escrow.go`) | Extended (Item reservation, error recovery on accept) |

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
- **ReAuth Middleware:** `REST()` wraps HTTP requests in a singleflight re-authentication middleware. If Steam returns 401 or 403, active requests are paused, `Refresh(ctx)` is triggered via OAuth2 finalization (`/jwt/finalizelogin`), cookies are updated, and failed requests retry automatically.
- **Auto-Refresh Loop:** `StartAutoRefresh(ctx, 6*time.Hour)` runs in the background to proactively refresh web cookies before Steam invalidates them.

### 2.2 2FA TOTP & Steam Guard Confirmations
- `internal/crypto/totp.go`: Implements Steam's custom alphanumeric base-32 alphabet (`23456789BCDFGHJKMNPQRTVWXY`) over HMAC-SHA1.
- `pkg/steam/guard/guard.go`:
  - Decodes identity and shared secrets from Base64 or Hex strings.
  - Automatically fetches and applies Steam Server Time Offset via `IAuthenticationService/GetPasswordRSAPublicKey`.
  - Generates confirmation hashes using HMAC-SHA1 with tag and timestamp.
  - Supports individual and batch mobile confirmations (`RespondToConfirmation`, `RespondToMultiple`).

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
- When `SendOffer` finishes (or fails), locks are released.

### 4.2 Error Recovery on `AcceptOffer`
Steam's `tradeoffer/{id}/accept` endpoint often returns HTTP 500 / 429 or network timeouts even when the trade was successfully processed on Steam's backend.
- When `community.PostFormTo[acceptResponse]` returns an error, `AcceptOffer` queries `GetOffer(ctx, offerID)` via `IEconService/GetTradeOffer`.
- If the offer state is `OfferStateAccepted` or `OfferStateInEscrow`, the operation succeeds without returning a false error.

### 4.3 Escrow Duration Checking
`escrow.GetEscrowDuration` fetches `https://steamcommunity.com/tradeoffer/{offerID}/` and parses embedded JavaScript variables:
- `g_DaysTheirEscrow = <days>;`
- `g_DaysMyEscrow = <days>;`
If either value is greater than 0, `CheckEscrow` reports an active trade hold.
