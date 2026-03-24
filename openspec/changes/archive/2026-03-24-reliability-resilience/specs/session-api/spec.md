## ADDED Requirements

### Requirement: Health check endpoint
The system SHALL expose a `GET /healthz` endpoint that returns HTTP 200 when the dashboard server is running and able to accept requests. This endpoint is used by Docker's health check mechanism to detect unresponsive containers.

#### Scenario: Server is healthy
- **WHEN** a GET request is made to `/healthz`
- **THEN** the server SHALL respond with HTTP 200 and a JSON body `{"status":"ok"}`

#### Scenario: Server is unresponsive
- **WHEN** the Go HTTP server has stopped accepting connections or is deadlocked
- **THEN** the `/healthz` request SHALL time out, and Docker's health check SHALL mark the container as unhealthy after the configured number of retries

#### Scenario: Health check does not require authentication
- **WHEN** a GET request is made to `/healthz` from any origin
- **THEN** the server SHALL respond without requiring any authentication headers or cookies
