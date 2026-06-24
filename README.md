# crowlink

A small, dependency-light URL shortener service written in Go.

`crowlink` accepts a long URL and returns a short code; visiting the short code
redirects to the original. It also tracks click analytics, supports custom
aliases and link expiry, and ships with rate limiting, a tiny landing page, and
a CLI client.

```
POST /shorten  {"url":"https://example.com/very/long/path"}
  -> {"code":"aB3x","short_url":"http://localhost:8080/aB3x"}

GET  /aB3x        -> 302 redirect to the original URL
GET  /aB3x/stats  -> {"code":"aB3x","url":"...","clicks":42,"created_at":"..."}
```

## Status

This repository is built incrementally, one funded GitHub issue at a time, by
[CleverCrow](https://clevercrow.io) — a Claude coding agent that plans, codes,
opens a draft PR, fixes CI, and responds to review feedback. Each issue is a
self-contained, test-backed step that builds on the previous one. Watch the
issues and pull requests to follow along.

## Design

- **Pure Go, low dependency** — stdlib `net/http` and `embed`, plus one embedded
  database (`go.etcd.io/bbolt`) for persistence. No cgo, no external services.
- **Interface-first storage** — a `Store` interface with in-memory and bbolt
  implementations, so the backend is swappable and handlers are tested against
  both.
- **Tested from the first commit** — `go build`, `go vet`, and `go test ./...`
  run in CI on every pull request.

## Development

```sh
go test ./...     # run the test suite
go vet ./...      # vet
go run .          # start the server (default :8080)
```
