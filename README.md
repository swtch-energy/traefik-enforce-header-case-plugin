# Traefik Enforce Header Case Plugin

![Build Status](https://github.com/clugg/traefik-enforce-header-case-plugin/workflows/Main/badge.svg) [![Latest Release](https://img.shields.io/github/v/release/clugg/traefik-enforce-header-case-plugin?include_prereleases&sort=semver)](https://github.com/clugg/traefik-enforce-header-case-plugin/releases)

A [Traefik](https://traefik.io/) middleware plugin which enforces the case of specified request and response headers.

This overrides Go (and, by extension, Traefik)'s default behaviour of [canonicalising header keys](https://pkg.go.dev/net/http#Header), which can be useful when working with HTTP servers/applications that are case-sensitive to certain headers.

## Configuration

### Static

```yaml
experimental:
  plugins:
    enforceHeaderCase:
      moduleName: github.com/clugg/traefik-enforce-header-case-plugin
      version: v0.1.0
```

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
