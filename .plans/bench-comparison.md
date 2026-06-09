# Benchmark Comparison: encoding/json → goccy/go-json

Replacing `encoding/json` with `github.com/goccy/go-json` yields significant improvements across
all three benchmarks on Apple M1 Max (arm64, `-benchtime=3s`). The most dramatic gain is in
`BenchmarkDecodeResponse`, which exercises the full wire-response decode path: latency drops by
**~67 %**, heap allocations shrink by **~41 %**, and allocation count falls by **~18 %**. The
`QueryService_Execute` benchmarks (which exercise the end-to-end service layer with a mock HTTP
handler) also improve substantially — sequential throughput improves by **~55 %** and parallel
throughput by **~21 %**, with allocation counts halved in both cases.

| Benchmark | Before ns/op | After ns/op | Δns/op | Before B/op | After B/op | ΔB/op | Before allocs/op | After allocs/op | Δallocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| BenchmarkDecodeResponse | 9,936,356 | 3,277,797 | -6,658,559 (-67.0 %) | 6,109,216 | 3,605,160 | -2,504,056 (-41.0 %) | 126,040 | 103,042 | -22,998 (-18.2 %) |
| BenchmarkQueryService_Execute_Sequential | 1,255 | 560 | -695 (-55.4 %) | 952 | 649 | -303 (-31.8 %) | 19 | 10 | -9 (-47.4 %) |
| BenchmarkQueryService_Execute_Parallel | 661 | 525 | -136 (-20.6 %) | 952 | 650 | -302 (-31.7 %) | 19 | 10 | -9 (-47.4 %) |

Environment: `goos: darwin`, `goarch: arm64`, `cpu: Apple M1 Max`, `-benchtime=3s`
