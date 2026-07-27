# health 健康度特性

- [HEALTH_SPEC.md](HEALTH_SPEC.md)：健康评分规格。
- [HEALTH_DESIGN.md](HEALTH_DESIGN.md)：health 与 stress 的边界和集成设计。
- [stress/README.md](stress/README.md)：显式高负载压测子特性。

普通 `catmonitor health` 只进行指标采集与评分；压测必须使用 `catmonitor health stress run` 显式启动。
