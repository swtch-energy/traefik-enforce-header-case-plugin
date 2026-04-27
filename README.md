# Traefik Enforce Header Case Plugin

![Build Status](https://github.com/swtch-energy/traefik-enforce-header-case-plugin/workflows/Main/badge.svg) [![Latest Release](https://img.shields.io/github/v/release/swtch-energy/traefik-enforce-header-case-plugin?include_prereleases&sort=semver)](https://github.com/swtch-energy/traefik-enforce-header-case-plugin/releases)

A [Traefik](https://traefik.io/) middleware plugin which enforces the case of specified request and response headers.

This overrides Go (and, by extension, Traefik)'s default behaviour of [canonicalising header keys](https://pkg.go.dev/net/http#Header), which can be useful when working with HTTP servers/applications that are case-sensitive to certain headers.

**What it does not do:** it only **re-spells the header name** (key) for headers that are **already present**. It does **not** add or inject a header. If the client does not send a request header, or the origin does not set a response header, the plugin has nothing to rename. To *create* a header, use Traefik’s built-in [headers](https://doc.traefik.io/traefik/middlewares/http/headers/) middleware (or your app) and, if you need a non-canonical name, place this plugin **after** the one that sets the value (order matters in the `middlewares` chain).

**`Sec-WebSocket-Key` (WebSocket):** Browsers and [RFC 6455](https://www.rfc-editor.org/rfc/rfc6455#section-11.3) use the spelling `Sec-WebSocket-Key` (capital **S** in the middle of “WebSocket”). Go’s `http.Header` instead uses `Sec-Websocket-Key` (only the first letter of the last segment is capitalized). Put the exact name you need for the next hop in `headers` (e.g. `Sec-WebSocket-Key`); the plugin re-keys the map entry from Go’s form to that spelling.

## Why the backend can still look “wrong” (especially with Go)

This plugin only changes the `http.Header` map **inside Traefik** before the next handler runs. It cannot change how your **application runtime** stores header names after it reads the bytes off the socket.

- **If the backend is Go** (`net/http` server): when the request is parsed, the server’s MIME header reader ([`readMIMEHeader`](https://go.dev/src/net/textproto/reader.go)) always runs the header **name** through the same canonicalization rules and uses that as the **map key**. So in `*http.Request`, the key is **always** Go’s canonical form (e.g. `Sec-Websocket-Key`), even if the bytes on the wire used `Sec-WebSocket-Key`. You should use `Header.Get("Sec-WebSocket-Key")` / `Get("sec-websocket-key")` for the value—lookup is case-insensitive. You **cannot** obtain RFC-exact header **name** spelling from `r.Header`’s keys in the standard library.

- **If the backend is not Go** and truly needs exact **name** bytes: confirm with a raw TCP/MITM capture; some stacks mirror Go’s behavior.

- **Traefik’s oxy** WebSocket path copies headers with the same string keys as the in-memory map ([`CopyHeaders`](https://github.com/vulcand/oxy/blob/v1.4.0/utils/netutils.go) assigns `dst[k] = src[k]`), and normal HTTP forwarding uses `Request.Clone`, which `Header.Clone`s key strings. The remaining gap is almost always **re-parsing** on the backend, not this plugin “failing” in Traefik.

## Traefik plugin catalog and `Unknown plugin` (404)

`experimental.plugins` does **not** pull arbitrary modules from GitHub. Traefik asks the [Plugin Catalog](https://plugins.traefik.io/) service, which only knows about repositories it has **indexed**. If you see `Unknown plugin` / 404, the module is not in the catalog yet (or the repo is ineligible).

Per the [official plugin checklist](https://plugins.traefik.io/create), you need **all** of the following for the catalog to pick up this repo (the index runs about **once per day**):

- **Public** GitHub repository (not private).
- **Not a fork** of another repo (forks are **excluded** from the catalog; use a standalone repository or import if you need catalog install).
- GitHub topic **`traefik-plugin`** on the repository.
- [`.traefik.yml`](.traefik.yml) at the repo root with a valid `testData` (already present in this project).
- Valid root [`go.mod`](go.mod) and a **git tag** for the version in your static config (e.g. `v0.1.1` must exist on GitHub).
- If the module has **non-stdlib** dependencies, they must be [vendored](https://go.dev/ref/mod#vendoring) and committed (this project has no external `require`, so that does not apply).

After you fix topics/fork/visibility and push the tag, wait for the next catalog run or use **local mode** (below) immediately.

### Local mode (no catalog; works for private repos and while waiting)

From [Working with Traefik Plugins](https://plugins.traefik.io/install): put the module under `./plugins-local` next to the Traefik process, and use `experimental.localPlugins` instead of `experimental.plugins` so Traefik loads source from disk instead of the catalog.

**Important:** The path is relative to the **Traefik process current working directory** (not necessarily where the binary lives). On systemd, set `WorkingDirectory=...`; in Docker, set `WORKDIR` or the container `command` / `workdir` so that `./plugins-local/src/...` exists from that directory.

**Manifest error (`open .../.traefik.yml: no such file or directory`):** The whole module tree must be present, including the hidden [`.traefik.yml`](.traefik.yml) at the plugin root. A partial copy (only `main.go` / `go.mod`) is not enough. If you use Docker, ensure `COPY` (or the image build context) does **not** skip dotfiles: many `.dockerignore` patterns or explicit `COPY main.go` omit `.traefik.yml`. As a check, from the same working directory Traefik uses, run:  
`test -f plugins-local/src/github.com/swtch-energy/traefik-enforce-header-case-plugin/.traefik.yml`  
(or adjust the org/repo segment to match your `moduleName`).

## Configuration

### Static (catalog; after the repo is indexed)

```yaml
experimental:
  plugins:
    enforceHeaderCase:
      moduleName: github.com/swtch-energy/traefik-enforce-header-case-plugin
      version: v0.1.1
```

### Static (local mode; same `moduleName` as in `go.mod`)

Working directory of the Traefik process must contain `plugins-local/src/<import path>/` (e.g. clone this repository to `plugins-local/src/github.com/swtch-energy/traefik-enforce-header-case-plugin`).

```yaml
experimental:
  localPlugins:
    enforceHeaderCase:
      moduleName: github.com/swtch-energy/traefik-enforce-header-case-plugin
```

Dynamic `plugin.enforceHeaderCase` blocks stay the same; only the static `experimental` section changes from `plugins` to `localPlugins`.

### Dynamic

In the following example, a router has been set up with middleware to ensure the `x-tEsT-hEAder` name is used exactly as it appears in the configuration: on the way to the service for incoming request headers, and for matching response headers on the way back to the client. This means a request to the router with any casing of that name (e.g. `x-test-header: 123` or `X-TEST-HEADER: 123`) is forwarded to the service with the header key `x-tEsT-hEAder: 123`, and a response that uses the canonical `X-Test-Header` key can be rewritten to the same configuration spelling before it reaches the client.

```yaml
http:
  middlewares:
    enforce-header-case:
      plugin:
        enforceHeaderCase:
          headers:
            - x-tEsT-hEAder

  routers:
    my-router:
      rule: Host(`localhost`)
      service: my-service
      middlewares:
        - enforce-header-case@file

  services:
    my-service:
      loadBalancer:
        servers:
          - url: 'http://127.0.0.1'
```
