# Performance baseline

Run the complete local benchmark suite with:

```bash
task benchmark
```

The suite uses in-memory MCP transports, fake credentials, and a local HTTP test server. It never contacts Beget. Each benchmark reports latency, allocated bytes, and allocation count. `BenchmarkMCPToolsList` also reports the serialized response size and tool count.

## Reference run

This diagnostic baseline was recorded on 2026-07-13 with Go 1.26.5 on Linux amd64. The processor was an AMD Ryzen 9 9950X3D. Values are not CI thresholds because scheduler and hardware differences can cause normal variation.

| Benchmark | Time per operation | Bytes per operation | Allocations |
| --- | ---: | ---: | ---: |
| Server startup | 3.935 ms | 2,636,676 | 70,932 |
| MCP initialize | 138.5 µs | 309,832 | 217 |
| MCP `tools/list` | 2.767 ms | 1,971,242 | 13,317 |
| Largest Cron input schema | 5.968 µs | 13,341 | 44 |
| Nested directives input schema | 5.838 µs | 13,734 | 55 |
| MCP tool call | 103.4 µs | 337,658 | 196 |
| Concurrent MCP tool calls | 70.02 µs | 337,508 | 199 |
| Local HTTP round trip | 27.25 µs | 10,171 | 115 |
| Concurrent local HTTP round trips | 33.83 µs | 37,779 | 146 |

The `tools/list` response contained all 66 tools and occupied 56,615 serialized bytes. Treat the table as a comparison point for profiling and regressions, not as a promise for other machines.
