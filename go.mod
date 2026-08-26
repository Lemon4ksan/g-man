module github.com/lemon4ksan/g-man

go 1.27.0

require (
	github.com/andygrunwald/vdf v1.1.0
	github.com/lemon4ksan/aoni v0.6.2-0.20260826201021-af75080ce45c
	github.com/lemon4ksan/foundation v0.0.0-20260826200314-de4763a9c376
	github.com/lemon4ksan/sein v0.0.0
	github.com/mitchellh/mapstructure v1.5.0
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10
	github.com/stretchr/testify v1.11.1
	golang.org/x/net v0.58.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/refraction-networking/utls v1.8.2 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.73.0 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/lemon4ksan/aoni => ../aoni
	github.com/lemon4ksan/foundation => ../foundation
	github.com/lemon4ksan/sein => ../server/sein
)
