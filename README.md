# Neo4j Query API Go SDK

## Overview

A Go client for the Neo4j Query API. Execute Cypher queries against Neo4j databases using plain HTTP — no Bolt protocol, no driver, no binary dependencies. The SDK mirrors the transformer pattern from the official Neo4j Go driver, so switching between them is straightforward.

```go
result, err := query.WithTransformer(client.Query, ctx,
    "MATCH (n:Person) RETURN n.name AS name", nil,
    query.EagerResultTransformer)
```

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Authentication](#authentication)
- [Configuration](#configuration)
- [API Flavors](#api-flavors)
- [Access Mode](#access-mode)
- [Streaming](#streaming)
- [Context and Timeouts](#context-and-timeouts)
- [Executing Queries](#executing-queries)
- [Transformers](#transformers)
  - [EagerResultTransformer](#eagerresulttransformer)
  - [Collect](#collect)
  - [Single](#single)
  - [Custom transformers](#custom-transformers)
- [Working with Records](#working-with-records)
- [Error Handling](#error-handling)
- [Best Practices](#best-practices)
- [CI & Releases](#ci--releases)

---

## Installation

```bash
go get github.com/neo4j-contrib/query-go-sdk
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    query "github.com/neo4j-contrib/query-go-sdk"
)

func main() {
    client, err := query.NewClient(
        query.WithBasicAuth("neo4j", "password"),
        query.WithBaseURL("http://localhost:7474"),
        query.WithTimeout(30*time.Second),
    )
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()

    ctx := context.Background()

    result, err := query.WithTransformer(
        client.Query,
        ctx,
        "MATCH (n:Person) RETURN n.name AS name LIMIT 10",
        nil,
        query.EagerResultTransformer,
    )
    if err != nil {
        log.Fatalf("Query failed: %v", err)
    }

    for _, rec := range result.Records {
        name, _ := rec.GetString("name")
        fmt.Printf("Name: %s\n", name)
    }
}
```

---

## Authentication

Authentication is required and mutually exclusive — pass either `WithBasicAuth` or `WithBearerToken`, not both.

### Basic authentication

```go
client, err := query.NewClient(
    query.WithBasicAuth("neo4j", "your-password"),
)
```

### Bearer token

```go
client, err := query.NewClient(
    query.WithBearerToken("your-bearer-token"),
)
```

---

## Configuration

| Option | Default | Description |
|---|---|---|
| `WithBaseURL(url)` | `http://localhost:7474` | Base URL of the Neo4j server |
| `WithDatabase(db)` | — | Target database name |
| `WithTimeout(d)` | 120s | Per-request timeout |
| `WithMaxRetry(n)` | 3 | Maximum retry attempts on transient failure |
| `WithLogger(l)` | warn to stderr | Structured `*slog.Logger` |
| `WithHTTPClient(c)` | default transport | Custom `*http.Client` (for mTLS, proxies, etc.) |
| `WithUserAgent(ua)` | `query-go-sdk/<version>` | Value of the `User-Agent` header |
| `WithMaxResponseSize(n)` | 10MB | Maximum response body size in bytes; returns an error if exceeded |
| `WithDefaultHeaders(map)` | — | Extra headers sent with every request; `Authorization`, `Content-Type`, `Accept`, and `User-Agent` are protected and cannot be overridden |
| `WithAPIFlavor(f)` | `FlavorQueryV2` | Select which HTTP API endpoint to target; see [API Flavors](#api-flavors) |
| `WithAccessMode(m)` | `AccessModeUnset` | Set the access mode sent with every request; see [Access Mode](#access-mode) |
| `WithStreamingSupport(b)` | `false` | Enable the Query API's streaming (JSON Lines) response format and unlock `ExecuteStream`; see [Streaming](#streaming) |

```go
client, err := query.NewClient(
    query.WithBasicAuth("neo4j", "password"),
    query.WithBaseURL("http://neo4j.example.com:7474"),
    query.WithDatabase("mydb"),
    query.WithTimeout(60*time.Second),
    query.WithMaxRetry(5),
    query.WithDefaultHeaders(map[string]string{
        "X-Request-Source": "my-service",
    }),
)
```

---

## API Flavors

The SDK supports two Neo4j HTTP APIs, selectable via `WithAPIFlavor`:

| Constant | Endpoint | Response format | Default |
|---|---|---|---|
| `query.FlavorQueryV2` | `/db/{db}/query/v2` | Typed JSON (`$type` envelopes) | yes |
| `query.FlavorLegacyHTTP` | `/db/{db}/tx/commit` | Plain JSON row format | — |

`FlavorQueryV2` is the default and requires no explicit configuration.

### Targeting the legacy HTTP API

Use `FlavorLegacyHTTP` to send queries to the older Cypher HTTP Transaction API. This is useful for performance comparison testing or for connecting to Neo4j versions that pre-date the Query API.

```go
client, err := query.NewClient(
    query.WithBasicAuth("neo4j", "password"),
    query.WithBaseURL("http://localhost:7474"),
    query.WithAPIFlavor(query.FlavorLegacyHTTP),
)
```

Everything else — transformers, records, error handling — works identically for both flavors.

#### Type mapping differences

The legacy API returns row values as plain JSON without typed envelopes. Scalars (`string`, `int64`, `float64`, `bool`, `nil`) decode identically to `FlavorQueryV2`. However, graph entities are returned as plain `map[string]any` instead of `*query.Node` / `*query.Relationship`, because the `/tx/commit` endpoint does not include element IDs or labels in the row format. `rec.GetNode` and `rec.GetRelationship` will return `(nil, false)` for those values. The `/tx/commit` endpoint also doesn't return `queryType`/timing metadata, so `Response.QueryType`, `ResultAvailableAfter`, and `ResultConsumedAfter` stay at their zero values for `FlavorLegacyHTTP`.

---

## Access Mode

The `WithAccessMode` option controls the access mode sent with every request. This hints to the server whether the operation requires write access or can be served by a read replica. With Neo4j Clusters, this option can be used to redirect queries to followers and mutations to the leader as the latter is the only cluster member who can make changes to the graph. This allows for the application to make load balancing decisions to avoid overwhelming the leader.

| Constant 
|---
| `AccessModeUnset` (default) 
| `AccessModeRead` 
| `AccessModeWrite` 

By default, no access mode is sent at all. In a cluster, this means server-side routing decides where to send the request: any cluster member can handle it if it turns out to be a read, but only the leader can handle a write, so server-side routing forwards the request to the leader whenever the operation turns out to require a write.

```go
// Read-only workload — may be routed to a read replica
client, err := query.NewClient(
    query.WithBasicAuth("neo4j", "password"),
    query.WithBaseURL("http://localhost:7474"),
    query.WithAccessMode(query.AccessModeRead),
)
```

```go
// Write workload — explicitly forces leader routing
client, err := query.NewClient(
    query.WithBasicAuth("neo4j", "password"),
    query.WithBaseURL("http://localhost:7474"),
    query.WithAccessMode(query.AccessModeWrite),
)
```

---

## Streaming

By default, `Execute` buffers the entire query result before returning it. `WithStreamingSupport(true)` switches the wire format to the Query API's streaming (JSON Lines) response and unlocks `QueryService.ExecuteStream`, which decodes records incrementally as they arrive instead of holding the full result in memory. This matters for large result sets, and lets a consumer start processing rows before the server has finished sending them.

```go
client, err := query.NewClient(
    query.WithBasicAuth("neo4j", "password"),
    query.WithBaseURL("http://localhost:7474"),
    query.WithStreamingSupport(true),
)
```

`ExecuteStream` returns a `*query.StreamResult`. Iterate over `Records()` with a standard Go range loop; the same `*query.Record` accessors described in [Working with Records](#working-with-records) apply to each row:

```go
result, err := client.Query.ExecuteStream(ctx, "MATCH (n:Person) RETURN n.name AS name", nil)
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Fields()) // ["name"] — available immediately, before any rows

for rec, err := range result.Records() {
    if err != nil {
        log.Fatal(err) // transport/decode error, or a *query.QueryErrors surfaced mid-stream
    }
    name, _ := rec.GetString("name")
    fmt.Println(name)
}

summary := result.Summary() // bookmarks, notifications, timings — populated once Records() is fully drained
fmt.Println(summary.Bookmarks)
```

`StreamSummary` carries the same `QueryType`, `ResultAvailableAfter`, and `ResultConsumedAfter` fields as `query.Response`/`EagerResult` — Neo4j reports this metadata identically whether or not streaming is enabled.

Breaking out of the loop early still releases the underlying HTTP connection — `Records()` closes it automatically whether iteration ends naturally, on error, or via an early `break`. Call `result.Close()` directly if you need to abandon a stream without iterating at all.

`WithStreamingSupport` is a client-wide setting: `Execute` is unaffected by it, and `ExecuteStream` returns an error if the client wasn't constructed with it enabled. It is not supported together with `WithAPIFlavor(query.FlavorLegacyHTTP)` — `NewClient` returns an error if both are set, since the legacy Cypher HTTP Transaction API has no streaming format.

Because `ExecuteStream` returns before the response body has been fully read, the request's timeout (`WithTimeout`, default 120s) bounds the *entire* stream lifetime — from the initial request through however long the caller takes to finish draining `Records()` — not just the time to the first byte. Raise `WithTimeout` for clients that stream very large results or consume them slowly.

---

## Context and Timeouts

Every operation accepts a `context.Context` as its first argument. The service applies its own timeout via `context.WithTimeout` on every call — whichever deadline is shorter (parent or service) wins.

```go
// Per-call deadline overrides the service timeout when shorter
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

resp, err := client.Query.Execute(ctx, "MATCH (n) RETURN count(n)", nil)
```

```go
// Cancellation propagates through to in-flight HTTP requests
ctx, cancel := context.WithCancel(context.Background())
go func() { <-shutdown; cancel() }()

_, err := client.Query.Execute(ctx, "MATCH (n) RETURN n", nil)
if errors.Is(err, context.Canceled) {
    log.Println("query cancelled")
}
```

---

## Executing Queries

### Direct execution

`client.Query.Execute` returns a `*query.Response` containing the raw decoded result.

```go
resp, err := client.Query.Execute(ctx, "RETURN 1 AS n", nil)
if err != nil {
    log.Fatal(err)
}

fmt.Println(resp.Fields)    // ["n"]
fmt.Println(resp.Rows)      // [[int64(1)]]
fmt.Println(resp.Bookmarks)
```

`query.Response` fields:

| Field | Type | Description |
|---|---|---|
| `Fields` | `[]string` | Ordered column names |
| `Rows` | `[][]any` | Decoded row values (see type mapping below) |
| `Notifications` | `[]query.Notification` | Advisory messages from Neo4j |
| `Bookmarks` | `[]string` | Transaction bookmarks |
| `QueryPlan` | `*query.PlanOperator` | Non-nil for EXPLAIN/PROFILE queries |
| `QueryType` | `string` | `"r"` (read), `"rw"` (read/write), `"w"` (write), or `"s"` (schema write) |
| `ResultAvailableAfter` | `time.Duration` | Time for the result to become available |
| `ResultConsumedAfter` | `time.Duration` | Time to fully consume the result |

### Passing parameters

```go
params := map[string]any{"name": "Alice", "age": 30}
resp, err := client.Query.Execute(ctx,
    "MATCH (n:Person {name: $name}) WHERE n.age >= $age RETURN n.name",
    params)
```

---

## Transformers

The transformer pattern mirrors the Neo4j Go driver's `neo4j.ExecuteQuery`. A transformer is a function that converts `*query.Response` into a typed Go value:

```go
type ResultTransformer[T any] func(*query.Response) (T, error)
```

Three built-in transformers cover the common cases. You can also write your own — see [Custom transformers](#custom-transformers).

```go
result, err := query.WithTransformer(svc, ctx, cypher, params, transformer)
```

### EagerResultTransformer

Collects all rows into a `*query.EagerResult`. Use for typical queries where the full result set fits in memory.

```go
result, err := query.WithTransformer(
    client.Query, ctx,
    "MATCH (n:Person) RETURN n.name AS name, n.age AS age",
    nil,
    query.EagerResultTransformer,
)
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Keys)          // ["name", "age"]
fmt.Println(len(result.Records))  // number of rows

for _, rec := range result.Records {
    name, _ := rec.GetString("name")
    age, _  := rec.GetInt64("age")
    fmt.Printf("%s is %d\n", name, age)
}
```

`EagerResult` also exposes `HasWarnings()` and `Warnings()` for advisory notifications, and the same `QueryType`, `ResultAvailableAfter`, and `ResultConsumedAfter` fields documented above for `query.Response`.

### Collect

Maps each row to a value using a function, returning `[]T`.

```go
type Person struct {
    Name string
    Age  int64
}

people, err := query.WithTransformer(
    client.Query, ctx,
    "MATCH (n:Person) RETURN n.name AS name, n.age AS age",
    nil,
    query.Collect(func(rec *query.Record) (Person, error) {
        name, _ := rec.GetString("name")
        age, _  := rec.GetInt64("age")
        return Person{Name: name, Age: age}, nil
    }),
)
```

### Single

Expects exactly one row; returns an error if zero or more than one row is returned.

```go
type Movie struct {
    Title    string
    Released int64
}

movie, err := query.WithTransformer(
    client.Query, ctx,
    "MATCH (m:Movie {title: $title}) RETURN m.title AS title, m.released AS released",
    map[string]any{"title": "The Matrix"},
    query.Single(func(rec *query.Record) (Movie, error) {
        title, _    := rec.GetString("title")
        released, _ := rec.GetInt64("released")
        return Movie{Title: title, Released: released}, nil
    }),
)
```

### Custom transformers

Because `*query.Response` is part of the public API, you can write your own `ResultTransformer` for cases that `EagerResultTransformer`, `Collect`, and `Single` don't cover.

```go
// A transformer that extracts a single column as a flat []string
func firstColumnStrings(resp *query.Response) ([]string, error) {
    out := make([]string, 0, len(resp.Rows))
    for _, row := range resp.Rows {
        if len(row) == 0 {
            continue
        }
        s, ok := row[0].(string)
        if !ok {
            return nil, fmt.Errorf("expected string, got %T", row[0])
        }
        out = append(out, s)
    }
    return out, nil
}

names, err := query.WithTransformer(
    client.Query, ctx,
    "MATCH (n:Person) RETURN n.name",
    nil,
    query.ResultTransformer[[]string](firstColumnStrings),
)
```

Custom transformers work with any named function whose signature matches `func(*query.Response) (T, error)`.

---

## Working with Records

`*query.Record` provides named field access that mirrors the Neo4j Go driver's `neo4j.Record`.

### Typed accessors

```go
s, ok    := rec.GetString("name")          // string
n, ok    := rec.GetInt64("count")          // int64
f, ok    := rec.GetFloat64("score")        // float64
b, ok    := rec.GetBool("active")          // bool
bs, ok   := rec.GetBytes("data")           // []byte
t, ok    := rec.GetTime("createdAt")       // time.Time
d, ok    := rec.GetDuration("lifespan")    // query.Duration
p, ok    := rec.GetPoint("location")       // query.Point
node, ok := rec.GetNode("n")               // *query.Node
rel, ok  := rec.GetRelationship("r")       // *query.Relationship
path, ok := rec.GetPath("p")               // query.Path
list, ok := rec.GetList("tags")            // []any
m, ok    := rec.GetMap("props")            // map[string]any
```

All accessors return `(zero, false)` when the field is absent or null.

### Graph entity types

`query.Node`, `query.Relationship`, and `query.Path` are part of the public API — you can use them in your own function signatures:

```go
func nodeLabel(n *query.Node) string {
    if len(n.Labels) > 0 {
        return n.Labels[0]
    }
    return ""
}

func relSummary(r *query.Relationship) string {
    return fmt.Sprintf("[%s]", r.Type)
}
```

`query.Node` fields: `ElementID string`, `Labels []string`, `Properties map[string]any`.  
`query.Relationship` fields: `ElementID`, `StartNodeElementID`, `EndNodeElementID string`, `Type string`, `Properties map[string]any`.

### Neo4j → Go type mapping

| Neo4j type | Go type |
|---|---|
| Null | `nil` |
| Boolean | `bool` |
| Integer | `int64` |
| Float | `float64` |
| String | `string` |
| ByteArray | `[]byte` |
| List | `[]any` |
| Map | `map[string]any` |
| Date / Time / DateTime / LocalTime / LocalDateTime | `time.Time` |
| Duration | `query.Duration` |
| Point (2D / 3D) | `query.Point` |
| Node | `*query.Node` |
| Relationship | `*query.Relationship` |
| Path | `query.Path` |

### Generic accessor

```go
val, ok := rec.Get("fieldName")  // returns (any, bool)
```

### Other helpers

```go
rec.Keys()           // []string — ordered field names
rec.Values()         // []any   — ordered decoded values
rec.AsMap()          // map[string]any
rec.IsNull("field")  // true if field present and null
```

### List helpers

```go
rawList, _ := rec.GetList("tags")
tags, ok   := query.StringList(rawList)   // []string
counts, ok := query.Int64List(rawList)    // []int64
scores, ok := query.Float64List(rawList)  // []float64
```

---

## Error Handling

### HTTP errors — `*query.Error`

Returned when the Neo4j server responds with a non-2xx status code.

```go
resp, err := client.Query.Execute(ctx, cypher, params)
if err != nil {
    var apiErr *query.Error
    if errors.As(err, &apiErr) {
        fmt.Printf("HTTP %d: %s\n", apiErr.StatusCode, apiErr.Message)

        switch {
        case apiErr.IsUnauthorized():
            log.Fatal("check credentials")
        case apiErr.IsNotFound():
            log.Fatal("endpoint not found")
        case apiErr.IsBadRequest():
            log.Fatal("bad request")
        }
    }
}
```

`query.IsNotFound(err)` is a convenience helper that unwraps the error chain.

### Neo4j query errors — `*query.QueryErrors`

Returned when Neo4j executes the request but reports one or more Cypher errors (syntax errors, constraint violations, etc.).

```go
resp, err := client.Query.Execute(ctx, "INVALID CYPHER", nil)
if err != nil {
    var qErr *query.QueryErrors
    if errors.As(err, &qErr) {
        for _, e := range qErr.Errors {
            fmt.Printf("[%s] %s: %s\n", e.Classification(), e.Title(), e.Message)
        }
    }
}
```

`QueryError` helper methods: `Classification()` (e.g. `ClientError`), `Category()` (e.g. `Statement`), `Title()` (e.g. `SyntaxError`).

### Context errors

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

_, err := client.Query.Execute(ctx, cypher, nil)
if errors.Is(err, context.DeadlineExceeded) {
    log.Println("query timed out")
}
if errors.Is(err, context.Canceled) {
    log.Println("query cancelled")
}
```

---

## Best Practices

### Secure credentials

```go
user := os.Getenv("NEO4J_USERNAME")
pass := os.Getenv("NEO4J_PASSWORD")

client, err := query.NewClient(
    query.WithBasicAuth(user, pass),
    query.WithBaseURL(os.Getenv("NEO4J_URL")),
)
```

### Context management

Always pass a context with an appropriate deadline or cancellation. Use `defer client.Close()` to release idle connections when the client is no longer needed.

### Error classification

Use `errors.As` to branch on `*query.Error` (HTTP-level) versus `*query.QueryErrors` (Cypher-level). Transient server errors (5xx) may be worth retrying; client errors (4xx) generally should not be.

---

## CI & Releases

Three GitHub Actions workflows manage CI and the release process.

### Workflows

| Workflow | Trigger | What it does |
|---|---|---|
| **CI** | Push to `main`, every PR | Runs tests with the race detector, golangci-lint, and `go build ./...` |
| **Changelog check** | Every PR | Fails if the PR changes `.go` files but has no entry in `.changes/unreleased/` |
| **Release** | Push of a `vX.Y.Z` tag | Gates on tests, extracts the changelog section, creates a GitHub Release |

### Making a release

Releases follow a three-step process. changie collects the unreleased fragment files and determines the correct semver bump automatically from the change kinds (`Added` → minor, `Fixed`/`Security` → patch, `Changed`/`Removed` → major).

There is **no manual version bump** required. `ClientVersion` uses `debug.ReadBuildInfo()` at runtime, scanning the consumer's dependency list (`info.Deps`) for this module's own entry to read the version the Go toolchain embedded when the consumer built their application — `info.Main` describes the consumer's own application, not this SDK, so it's deliberately not used. It falls back to `"development"` only when this module can't be found in `Deps` at all, e.g. local and test builds of this repository itself.

**1. Batch and merge the changelog**

```bash
changie batch   # collects .changes/unreleased/*.yaml → .changes/vX.Y.Z.md
changie merge   # folds that file into CHANGELOG.md
```

**2. Commit and tag**

```bash
git add CHANGELOG.md .changes/
git commit -m "chore: release vX.Y.Z"
git tag vX.Y.Z
git push origin main --tags
```

**3. Workflow takes over**

Pushing the tag fires the Release workflow, which:
- Runs `go test -race ./...` — the release is aborted if any test fails
- Extracts the `## vX.Y.Z` section from `CHANGELOG.md`
- Creates a GitHub Release with that text as the release notes

Because this is a Go module with no compiled binaries, the tag itself is what consumers reference:

```bash
go get github.com/neo4j-contrib/query-go-sdk@vX.Y.Z
```

### Adding a changelog entry

Every PR that changes Go source files needs a changie fragment. Run:

```bash
changie new
```

Choose a kind and write a one-line summary, then commit the generated `.yaml` file alongside your code changes. The Changelog check workflow will fail the PR if this step is skipped.

To bypass the check for a PR that genuinely needs no entry (docs-only, CI-only, or test-only changes), add the **`no-changelog`** label to the PR.

---

## License

See [LICENSE](LICENSE) file for details.
