# Benchmark results

`python3 bench/bench.py -n 20`, run 2026-09-26 on 6.18.33.2-microsoft-standard-WSL2, crun 1.28,
libkrun 1.19.0, pydantic-monty 1.0.0. Wall-clock latency from the host,
median of 20 runs after one warm-up. The "python" workload is
`sum(i * i for i in range(100_000))`.

```
tool                         isolation                 noop median  python median  python p95
host process                 none                          0.82 ms       19.13 ms    24.33 ms
podman + runc                container                   471.68 ms      470.99 ms   564.43 ms
podman + krun                microVM                    1624.06 ms     1844.00 ms  1899.62 ms
bluebox run (cold)           microVM                    1495.38 ms     1581.63 ms  1657.43 ms
bluebox run (warm: 2)        microVM, fresh per run       55.22 ms      183.21 ms   208.26 ms
bluebox exec (up)            microVM, persistent          15.63 ms       69.80 ms    76.34 ms
monty, new session           interpreter subprocess        0.55 ms       14.44 ms    19.78 ms
monty, same session          interpreter subprocess        0.24 ms       13.71 ms    16.19 ms
```

boxd was not measured (it is a hosted service). Its published figures are a
fresh boot in under 10ms, a fork in under 200ms and a resume in under 1ms,
with no stated hardware or method.
