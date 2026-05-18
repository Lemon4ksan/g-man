<div align="center">

# ⚙️ Steam Core Orchestrator

### The Brain of G-man: Unified Transport, Auth, and Protocols

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

The `steam` package is the foundational orchestrator of the G-man framework. It abstracts away the massive complexity of Valve's backend infrastructure by unifying three distinct environments into a single, thread-safe `steam.Client`:

1. **Persistent CM Connections**: Low-latency binary communication via Connection Manager Sockets (TCP/WebSockets).
2. **WebAPI Services**: Standardized RPC calls over HTTP (`api.steampowered.com`).
3. **Steam Community**: Defensive scraping and interaction with `steamcommunity.com`.

## ⚡ Core Systems Deep Dive

### 1. 🚦 Dual-Stack Transport Routing
G-man implements **Protocol Agnosticism**. When you send a request using the `Service()` router, you do not need to specify the transport layer. 

The router dynamically evaluates the request:
* If the bot is actively connected to the CM Socket and the request is a supported `EMsg`, it transmits via the **socket** for zero-overhead, real-time speed.
* If the socket drops, or the endpoint requires HTTP (like legacy WebAPIs), it transparently falls back to the **HTTP WebAPI client**.

### 2. 🔄 Self-Healing Authentication (OAuth2)
Valve uses a complex, layered auth system involving Refresh Tokens, JWT Access Tokens, and legacy Web Cookies.
The `Client` manages this entirely in the background:
* **Background Monitoring**: It actively monitors token expiration times.
* **Atomic Refreshes**: If a `401 Unauthorized` or cookie expiration occurs mid-request, the `Client` pauses the failing request, negotiates a new OAuth2 token, regenerates the cookies, and securely resumes the original request.
* **Invisible to Devs**: Your high-level code never has to manually retry a login due to a timeout.

### 3. 📡 Event-Driven Integration
The `steam` package utilizes the lock-free `pkg/bus` to broadcast its internal state. You can hook into the lifecycle events without modifying the core client.

**Available Core Events include:**
* `LoggedOnEvent` / `DisconnectedEvent`
* `SessionUpdatedEvent`
* `IncomingPacketEvent` (Raw `EMsg` interception)
* `WebAPIKeyRegisteredEvent`

## 🚀 Quickstart Example

Here is how you initialize the orchestrator, log in, and execute a Unified RPC call:

```go
package main

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"google.golang.org/protobuf/proto"
)

func main() {
	ctx := context.Background()

	// 1. Initialize Structured Logging
	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	// 2. Initialize the Orchestrator with default settings and logger
	cfg := steam.DefaultConfig()
	client, err := steam.NewClient(cfg, steam.WithLogger(logger))
	if err != nil {
		logger.Error("Failed to initialize Steam client", log.Err(err))
		return
	}
	defer func() {
		_ = client.Close()
		client.Wait()
	}()

	// 3. Prepare Login Details
	details := &auth.LogOnDetails{
		AccountName: "your_username",
		Password:    "your_password",
		// Optional: TwoFactorCode or AuthCode
	}

	// 4. Resolve the Optimal Connection Manager (CM) Server via Directory Service
	dir := directory.New(client.Service())
	server, err := dir.GetOptimalCMServer(ctx)
	if err != nil {
		logger.Error("Failed to resolve optimal CM server", log.Err(err))
		return
	}

	// 5. Connect to CM Servers and Authenticate
	logger.Info("Connecting to Steam CM network...")
	if err := client.ConnectAndLogin(ctx, server, details); err != nil {
		logger.Error("Login failed", log.Err(err))
		return
	}
	defer func() {
		_ = client.Disconnect()
	}()

	// 6. Execute a Unified RPC call using the smart router
	req := &pb.CPlayer_GetNickname_Request{
		Steamid: proto.Uint64(76561198000000000), // Target SteamID
	}
	
	// The Service() router automatically decides optimal transport (Socket vs HTTP)
	res, err := service.Unified[pb.CPlayer_GetNickname_Response](ctx, client.Service(), req)
	if err != nil {
		logger.Error("RPC call failed", log.Err(err))
		return
	}

	logger.Info("Target Nickname retrieved successfully", log.String("nickname", res.GetNickname()))
}
```

### 🔑 Key Implementation Highlights

When building a production-grade bot (like the [trading bot example](examples/trading_bot/main.go)), you should leverage the core architecture patterns shown below:

1. **Module Injection (Functional Options)**: Core modules (e.g. TF2, Backpack, Schema) should be passed during client instantiation using functional options instead of manual registration:
   ```go
   client, err := steam.NewClient(cfg,
       steam.WithLogger(logger),
       tf2.WithModule(),
       backpack.WithModule(),
       webtrading.WithModule(webtrading.Config{PollInterval: 30 * time.Second}),
   )
   ```

2. **Decoupled Event Handling**: Avoid synchronous blockers and utilize G-man's central thread-safe event bus to orchestrate flows such as Steam Guard prompts or successful logon confirmations:
   ```go
   sub := client.Bus().Subscribe(&auth.LoggedOnEvent{}, &auth.SteamGuardRequiredEvent{})
   go func() {
       for event := range sub.C() {
           switch ev := event.(type) {
           case *auth.LoggedOnEvent:
               logger.Info("Login successful!", log.Uint64("steam_id", ev.SteamID))
           }
       }
   }()
   ```

3. **Production Middleware Integration**: Build cohesive, layered trading pipelines (via onion-style middlewares) by integrating external modules, price indices, and currency managers dynamically.
