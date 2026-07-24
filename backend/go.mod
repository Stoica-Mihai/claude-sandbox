module claude-sandbox-backend

go 1.26.1

require (
	claude-sandbox-api v0.0.0
	github.com/gorilla/websocket v1.5.3
)

require claude-sandbox-sessiond v0.0.0

require golang.org/x/sys v0.47.0 // indirect

replace claude-sandbox-api => ../shared

replace claude-sandbox-sessiond => ../sessiond
