module claude-frontend

go 1.26.1

require (
	claude-sandbox-api v0.0.0
	github.com/gorilla/websocket v1.5.3
)

replace claude-sandbox-api => ../shared
