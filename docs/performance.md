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
| Server startup | 12.93 ms | 7,096,877 | 192,975 |
| MCP initialize | 123.6 µs | 308,325 | 215 |
| MCP `tools/list` | 9.510 ms | 6,621,932 | 40,769 |
| MCP capability resource | 205.7 µs | 228,522 | 108 |
| Largest Cron input schema | 6.541 µs | 13,374 | 44 |
| Nested directives input schema | 6.013 µs | 13,756 | 55 |
| MCP tool call | 106.5 µs | 340,383 | 256 |
| Concurrent MCP tool calls | 63.14 µs | 340,472 | 258 |
| Local HTTP round trip | 27.37 µs | 10,302 | 116 |
| Concurrent local HTTP round trips | 32.65 µs | 36,057 | 146 |

The `tools/list` response contained all 66 tools and occupied 144,736 serialized bytes, including operation-specific input and output schemas. The optional capability resource occupied 4,729 serialized bytes, about 31 times less, and is read only when routing remains unclear. Treat the table as a comparison point for profiling and regressions, not as a promise for other machines.
