# Performance baseline

Run the complete local benchmark suite with:

```bash
task benchmark
```

The suite uses in-memory MCP transports, fake credentials, and a local HTTP test server. It never contacts Beget. Each benchmark reports latency, allocated bytes, and allocation count. The MCP catalog benchmarks also report their serialized response sizes, and `BenchmarkMCPToolsList` reports the tool count.

## Reference run

This diagnostic baseline was recorded on 2026-07-13 with Go 1.26.5 on Linux amd64. The processor was an AMD Ryzen 9 9950X3D. Values are not CI thresholds because scheduler and hardware differences can cause normal variation.

| Benchmark | Time per operation | Bytes per operation | Allocations |
| --- | ---: | ---: | ---: |
| Server startup | 14.14 ms | 8,384,961 | 229,131 |
| MCP initialize | 123.3 µs | 309,996 | 216 |
| MCP `tools/list` | 11.19 ms | 7,563,809 | 48,158 |
| MCP capability resource | 121.6 µs | 228,727 | 109 |
| Largest Cron input schema | 7.715 µs | 15,072 | 52 |
| Nested directives input schema | 7.050 µs | 14,976 | 60 |
| MCP tool call | 96.50 µs | 340,272 | 256 |
| Concurrent MCP tool calls | 60.36 µs | 340,332 | 258 |
| Local HTTP round trip | 26.54 µs | 10,246 | 116 |
| Concurrent local HTTP round trips | 35.92 µs | 38,120 | 147 |

The `tools/list` response contained all 67 tools and occupied 167,349 serialized bytes, including operation-specific input and output schemas and the dry-run result contract for every mutation. The optional capability resource occupied 4,759 serialized bytes, about 35 times less, and is read only when routing remains unclear. Treat the table as a comparison point for profiling and regressions, not as a promise for other machines.

CI intentionally has no latency or allocation gate yet. These values are diagnostic until repeated runs across stable runners establish normal variance and a defensible tolerance. Deterministic contract checks such as tool count and serialized catalog size remain regular tests; a timing gate should be added only after the baseline is demonstrably stable.
