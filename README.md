# internctl

Command-line client for the internal management platform.

## Status

Current scope:

- device-code login
- keyring-first token storage with file fallback
- `whoami`
- `logout`
- `vlans list|create|update|delete`
- `devices list|create|update|delete`

## Usage

```bash
internctl login --server http://localhost:18080
internctl whoami
internctl vlans list
internctl vlans create --name iot --vlan-id 20 --description "IoT devices" --active
internctl devices create --name "Kitchen TV" --mac-address aa:bb:cc:dd:ee:ff --vlan-id 1
internctl logout
```

Global flags:

- `--profile`
  - profile name
  - default: `default`
- `--config-dir`
  - override config directory
  - default: `~/.intern`

`login` flags:

- `--server`
  - server base URL
  - default if no saved profile value exists: `https://intern.corp.example.com`
- `--token-backend`
  - one of `auto`, `keyring`, `file`
  - default: `auto`
- `--client-name`
  - optional override for the device-code client name
  - default: local hostname

Config layout:

- config: `~/.intern/config.json`
- file-backed sessions: `~/.intern/profiles/<profile>/session.json`

## OpenAPI

The generated client in `internal/api` is sourced from the vendored spec at `api/openapi.yaml`.

If you are working in the multi-repo workspace and want to refresh that snapshot from `intern-api`, run:

```bash
./scripts/sync-openapi.sh
go generate ./internal/api
```

## Local checks

```bash
go generate ./internal/api
go test ./...
go build ./cmd/internctl
docker buildx build --platform linux/amd64,linux/arm64 --output=type=cacheonly .
```
