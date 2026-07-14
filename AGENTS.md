# AGENTS.md

Guide for OpenCode sessions working in this repo. Verified against code, not docs.

## Project

CATMonitor — Go daemon that collects server metrics (CPU/memory/disk/GPU/NPU/network) and scores server health. Cross-platform Linux + Windows via Go build tags. Single external dependency: `gopkg.in/yaml.v3`. No CGo — GPU/NPU data comes from shelling out to `nvidia-smi` / `npu-smi`.

Module: `github.com/Computing-Availability-Tools/CATMonitor`. Entry point: `cmd/catmonitor/main.go`.

## Dev commands

```bash
make build            # go build -o bin/catmonitor ./cmd/catmonitor
make test             # go test ./...
make test-verbose     # go test -v ./...
make test-coverage    # go test -cover ./...
make lint             # go vet ./...   (only linter; no formatter step)
make clean
make install          # builds + copies binary/config to /usr/local/bin + /etc/catmonitor (needs sudo)
```

Single test / package:

```bash
go test ./internal/collectors/cpu/ -run TestCalculateUsage -v
go test ./internal/collectors/cpu/
```

Suggested order after changes: `make lint && make build && make test`.

## Toolchain & build quirks

- `go.mod` pins `go 1.23.4` — this is the real minimum (README/SPEC say 1.21+; ignore them).
- Cross-compile to verify platform-tag split: `GOOS=windows go build ./...`. Both targets must build clean.
- Windows code calls `kernel32.dll`/`iphlpapi.dll` via `syscall.NewLazyDLL`; there is no Windows toolchain requirement because no CGo.

## Cross-platform file convention

Each collector under `internal/collectors/<component>/` is split by build tag:

- `<component>.go` — shared: struct, `Collect()`, metric defs, delta logic, `init()` registration
- `<component>_linux.go` — reads `/proc`, `/sys`, `syscall.Statfs`, `dmesg`, `smartctl`
- `<component>_windows.go` — kernel32.dll / PowerShell
- `<component>_test.go` — usually `//go:build linux`

Exceptions: `gpu/` and `npu/` are single-file (`gpu.go`, `npu.go`) because `nvidia-smi`/`npu-smi` work the same on both platforms via `os/exec`.

Platform default paths live in `internal/platform/` (`platform_linux.go`, `platform_windows.go`).

## Adding a collector

1. Create `internal/collectors/<name>/<name>.go` (+ `_linux.go`/`_windows.go` as needed).
2. Implement `collector.Collector` (Name, Component, Collect, Priority, DefaultInterval, DefaultEnabled).
3. Register in an `init()` via `collector.DefaultRegistry.Register(New())`.
4. Add a blank import in `cmd/catmonitor/main.go` (e.g. `_ ".../internal/collectors/<name>"`) — the scheduler only discovers collectors imported there.
5. The core (`internal/collector`, `internal/health`) stays untouched by design.

## Testing

- Tests are co-located with packages as `*_test.go`. The `tests/` directory holds **only testdata**, not a framework — `SPEC.md` references `tests/framework.go` but it does not exist.
- Linux-collector tests (`cpu`, `disk`, `memory`, `network`) carry `//go:build linux` and will not run under `GOOS=windows`. `gpu`/`npu` tests and `internal/health` tests have no build tag and run everywhere.
- Linux tests point collectors at fixture paths via setters (e.g. `SetProcPath`) to `../../../tests/testdata/proc` and `../../../tests/testdata/sys` — paths are relative to each collector package, so keep that depth in mind if you move packages.
- `tests/testdata/` also has `nvidia-smi-output.txt` and `npu-smi-output.txt` for parsing tests.
- `docs/test_report.md` is a human-written per-phase report, not generated.

## CLI surface (trust code over README)

Implemented in `cmd/catmonitor/main.go`: commands `daemon`, `collect`, `health`, `list`, `version`; flags `-c/--config`, `-o/--output` (`json|table`). Default with no subcommand currently **panics** in `loadConfig` (indexes `os.Args[2:]`) — always run `catmonitor daemon` explicitly.

README/SPEC mention a `status` command and `--component`, `-i/--interval`, `-d/--data-dir`, `-v/--verbose` flags and `yaml` output format. **These are not implemented.** Don't rely on them; if a task needs them, they must be added.

## Config & storage

- YAML config. Defaults in `internal/config/config.go` `Default()`. A missing config file returns defaults silently (no error).
- Default config path is platform-specific via `internal/platform`; env overrides: `CATMONITOR_CONFIG`, `CATMONITOR_DATA_DIR`.
- Storage is JSONL: one file per component per day, `<component>_YYYY-MM-DD.jsonl` under `data_dir` (default `/var/lib/catmonitor/data`). Rotation is daily; `max_file_age` cleanup is configured but not enforced in `internal/storage` yet.
- Health scoring: weight schemes in `internal/health/rules.go`. `Evaluate()` auto-switches to the accelerated scheme when `gpu` or `npu` metrics are present.
- Daemon shuts down on SIGINT/SIGTERM (Linux). `scripts/install.sh` installs a systemd unit.

## Docs map

- `SPEC.md` — requirements (note: some claims have drifted from code)
- `DESIGN.md` — architecture and the collector/registry/scheduler design
- `docs/CATMonitor_indi_list.md` — full metric catalog (37 metrics across 6 components)
- `Release_Notes.md` — changelog; prepend new versions at the top, keep history
