module github.com/lemon4ksan/g-man

go 1.25.4

require (
	github.com/PuerkitoBio/goquery v1.12.0
	github.com/andygrunwald/vdf v1.1.0
	github.com/gorilla/websocket v1.5.3
	github.com/lemon4ksan/aoni v0.5.1-0.20260724180728-5fa2fc98b84d
	github.com/lemon4ksan/aoni/fast v0.0.0
	github.com/lemon4ksan/miyako v0.2.1-0.20260718175055-4d923876d502
	github.com/mitchellh/mapstructure v1.5.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/net v0.57.0
	golang.org/x/time v0.15.0
	google.golang.org/protobuf v1.36.11
)

require github.com/goccy/go-json v0.10.6

require (
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/andybalholm/cascadia v1.3.4 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.60.0 // indirect
	github.com/refraction-networking/utls v1.8.2 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.72.0 // indirect
	github.com/valyala/fastrand v1.1.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/lemon4ksan/aoni => ../aoni
	github.com/lemon4ksan/aoni/fast => ../aoni/fast
)
