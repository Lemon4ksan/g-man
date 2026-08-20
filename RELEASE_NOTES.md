# Release Notes

## v0.14.0 (2026-08-03)

### New Features

* **Commands System FSM & BBCode**: Added Finite State Machine (FSM) execution, custom command middleware, BBCode parsing module (`pkg/bbcode`), and chat reaction event handling.
* **Typed AppData**: Added typed `AppData` definitions for Steam community inventory models.
* **Persona Status Helper**: Added persona status helper for managing online rich presence state.

### Performance & Optimizations

* **High-Performance Client**: Integrated `aoni` fast HTTP client to accelerate transport request layers.
* **Fast JSON Parsing**: Replaced standard `encoding/json` with `goccy/go-json` across the client for faster encoding and decoding.
* **Allocation Reduction**: Optimized core components and frame processing to minimize heap allocations.

### Refactoring & Architecture

* **Miyako Logger Migration**: Removed internal `pkg/log` package in favor of the `miyako` logging library.
* **Trade Processor Reorganization**: Moved trade web processor from `pkg/trading/processor` into `pkg/behavior/processor`.
* **Documentation & Codebase Cleanup**: Streamlined documentation files across package subdirectories and removed unused internal scripts.

### Bug Fixes & Stability

* **JSON Parsing & Orchestrator**: Resolved JSON decoding edge cases and improved behavior orchestrator stability.
* **Post-Optimization Client Stability**: Addressed stability issues and race conditions following performance and memory allocation changes.
* **Directory Package Fix**: Corrected typo in filename `pkg/steam/sys/directory/directory.go`.

## v0.13.0 (2026-07-03)

### New Features

* Added steam persona status helper for making custom rich presence updates.

### Refactoring & Modernization

* Upgraded `github.com/lemon4ksan/aoni` to `v0.4.0` and migrated community, market, profile, guard, friends, websession, and directory packages to use `GetTo`, `PostTo`, and `PostFormTo` wrappers, eliminating manual body-closing boilerplate.
* Extracted `HistoryParser` and trade history HTML/JS parsing logic from `pkg/steam/community/inventory/inventory.go` into a new modular file `pkg/steam/community/inventory/history.go`.
* Implemented `SteamErrorsValidator` to handle redirect loops and common Steam errors via request middleware in `pkg/steam/community/client`.

### Improvements & Cleanup

* Exported `DefaultDomains` in `pkg/steam/auth/websession/websession.go`.
* Replaced verbose checks with `generic.Ternary` and `generic.Coalesce` from `miyako` across auth, community, profile, and friends packages.
* Added `make extract` target and removed verbose flags from race test runner.

## v0.12.0 (2026-07-02)

### Bug Fixes

* **Test Suite Data Race**: Fixed data races and timing issues in test suites.
* **Persona State Background Set**: Fixed background routine to correctly set persona state on login.

## v0.11.2 (2026-07-01)

### Bug Fixes

* **Guard Token Alignment**: Standardized action tags for token generation and requests.

## v0.11.1 (2026-07-01)

### Bug Fixes

* **Confirmation Rejection**: Fixed incorrect action tag resulting in confirmation rejection in mobile guard.

## v0.11.0 (2026-06-30)

### Bug Fixes

* **Aoni GetJSON Match**: Updated community requester to correctly match GetJSON request signatures.
* **Chat Legacy Events**: Fixed issue where chat did not subscribe to legacy message events.

## v0.10.1 (2026-06-30)

### Bug Fixes

* **Modern Notifications**: Subscribed web processor to modern notifications instead of legacy notifications.

## v0.10.0 (2026-06-30)

### Refactoring & Architecture

* **Steam Client Modularization**: Reorganized core Steam client into modular packages under `pkg/steam/client` (`modules`, `router`, `session`) and added dedicated notification management in `pkg/steam/sys/notifications`.
* **Community Client Restructuring**: Restructured Community client into `pkg/steam/community/client` and separated request models across subpackages (`market`, `inventory`, `openid`, `profile`).
* **Test Suite & Mocks Consolidation**: Centralized test mocks and helpers into `test/mock` and modernized unit test coverage across trading, socket connector, auth, and guard modules (94%).
* **BVDF & Encoding Cleanup**: Refactored BVDF parsing and encoding utilities in `pkg/steam/encoding`.

### Bug Fixes & Stability

* **Trading Processor Retry Behavior**: Added early exit for `ErrEscrowNotFound` in trading web processor retry routine to prevent redundant retries on unrecoverable escrow errors (`pkg/trading/web/processor`).
* **Test Data Race Fixes**: Resolved race conditions and channel synchronization issues in test suites (`session`, `connector`, `client`).

### Documentation

* **Architecture Documentation**: Updated `README.md`, `README_RU.md`, and package documentation (`pkg/doc.go`, `pkg/README.md`) to align with the modularized client architecture and community package structure.

## v0.9.0 (2026-06-24)

### Concurrency & Concurrency Stability

* **Session Manager Data Race Fix**: Fixed a data race and potential nil pointer dereference on the `web` session pointer during periodic verification checks in the background refresh loop (`pkg/steam`).
* **Connection Reconnect & Session Lock Fixes**: Synchronized internal clients and session manager during reconnection routines (`pkg/steam`).

### Features

* **UpdateServers Endpoint**: Added `UpdateServers` method to `SocketProvider` interface to dynamically update Steam Connection Manager (CM) servers (`pkg/steam`).

### Improvements

* **Randomized CM Server Selection**: Replaced sequential selection of Steam CM servers with random selection in `connector` to improve load balancing (`pkg/steam/socket/connector`).
* **Trade Offer Acceptance**: Shifted `AcceptOffer` request execution from raw web client to the community client to ensure unified handling of session credentials (`pkg/trading/web`).
* **Graceful Module Cleanup**: Added support for gracefully closing registered client modules implementing `io.Closer` when the Steam client stops (`pkg/steam`).

### Bug Fixes

* **Silent Failures on Market Order Cancellations**: Added response success checking (`Success` field verification) in `CancelBuyOrder` and `CancelSellOrder` methods to avoid silently ignoring API-level failures (`pkg/steam/community/market`).
* **Session ID in Avatar Uploads**: Added the required `sessionid` parameter to the multipart form upload data in `UploadAvatar` (`pkg/steam/community/profile`).
* **Raw Response Parsing in Unblock Communication**: Restored raw request handling in `UnblockCommunication` to handle raw text `"OK"` responses instead of failing on JSON decoding (`pkg/steam/social/friends`).
* **Orphan Heartbeat Goroutines**: Fixed an issue where heartbeat routines persisted after socket disconnection by implementing `heartbeatCancel` (`pkg/steam/socket`).
* **Flaky Test Heartbeat Failures**: Increased timeout for heartbeat channel validation in `TestSocket_Heartbeat` to improve test reliability under system load (`pkg/steam/socket`).

## v0.8.0 (2026-06-24)

### Improvements

* Replaced custom event bus, jobs, and memory storage implementations with the external `miyako` library.
* Added unexpected HTML content check to `SteamJSONDecoder` (`pkg/steam/encoding`).
* Added `PersonaState` configuration option for configuring client presence.

### Bug Fixes

* Guaranteed a disconnect call during connection monitoring in `monitorConnection` (`pkg/steam/socket/connector`).
* Removed TCP read timeout configuration (`pkg/network`).
* Removed explicit sleep calls in websession tests (`pkg/steam/auth/websession`).
* Fixed machine ID generation to generate valid values compliant with standard client expectations.
* Fixed hardcoded device friendly name to use the system hostname instead.

### Documentation

* Replaced C-style comments with standard line comments across package documentation files (`pkg/.../doc.go`).
