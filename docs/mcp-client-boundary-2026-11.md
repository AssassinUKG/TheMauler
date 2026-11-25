# MCP client boundary

TheMauler supports external Model Context Protocol servers through one compact, policy-owned `mcp`
tool. MCP is an integration boundary, not Mauler's internal tool runtime.

## Transports

- `streamable_http`: current Streamable HTTP, including endpoints such as
  `http://192.168.1.53:9876/mcp`.
- `sse`: compatibility transport for legacy HTTP+SSE servers, such as
  `http://192.168.1.53:9876`.
- `stdio`: a directly launched executable with newline-delimited JSON-RPC over stdin/stdout.

Network endpoints must be `localhost`, loopback, or a literal private-LAN IP. Mauler connects
directly and never creates an SSH tunnel, port forward, relay, or ambient proxy route. For the
PortSwigger server bound to `0.0.0.0:9876`, restrict the work-PC firewall rule to the trusted LAN.

## Trust and secrets

- The remote server's tool descriptions, schemas, and results are untrusted data. They cannot add
  native Mauler tools, expand scope, authorize actions, or override the control plane.
- Every `mcp` action is confirmation-gated. Only an exact operator-approved safe rule can bypass the
  prompt.
- HTTP bearer values are resolved from `bearer_token_env` and never returned to React.
- stdio children receive a bounded base environment plus explicitly allowlisted environment names.
- HTTP clients ignore ambient proxy settings, reject cross-origin redirects/endpoints, and cap SSE
  events, tool lists, schemas, text, and structured results.
- Image, audio, and binary resource results enter prompt context only as media type, byte count, and
  SHA-256 digest.

## PortSwigger LAN example

In Settings > MCP, enable MCP and add one of:

```text
ID: portswigger
Transport: Streamable HTTP (/mcp)
URL: http://192.168.1.53:9876/mcp
HTTP Host override: localhost:9876
HTTP Origin override: http://localhost:9876
```

or, for the legacy transport:

```text
ID: portswigger_sse
Transport: Legacy SSE
URL: http://192.168.1.53:9876
```

Use **Save and test** to perform initialize plus `tools/list`. Use only one transport entry for the
same server unless there is a specific compatibility reason to retain both.

PortSwigger 1.1.2 currently applies a localhost-only Host/Origin allowlist even when its listener is
bound to `0.0.0.0`. The explicit compatibility overrides satisfy that check while the socket still
connects directly to `192.168.1.53`; they do not create a tunnel, proxy, relay, or DNS route.
