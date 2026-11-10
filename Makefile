.PHONY: run build build-linux test tidy

# Desarrollo local (sin OAuth, sesión automática como admin).
run:
	DEV_AUTH_BYPASS=1 go run ./cmd/server

# Desarrollo local CON login real de Google (lee credenciales de .env).
run-oauth:
	@set -a; . ./.env; set +a; go run ./cmd/server

test:
	go test ./...

tidy:
	go mod tidy

# Binario para el host actual.
build:
	CGO_ENABLED=0 go build -o bin/server ./cmd/server

# Binario estático para el VPS Debian 11 (amd64).
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/server-linux-amd64 ./cmd/server
