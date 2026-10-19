#!/usr/bin/env bash
# Thin launcher: the canonical Compose files and generator remain the owners
# of deployment configuration. This script does not install/build/run a Burn.
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
HARDWARE=generic
MODE=monitoring
PROJECT=catmonitor
GENERATED=
CONFIG=
PUBLIC_HOST=
BACKEND=auto
OVERLAYS=()

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
usage() {
    cat <<'EOF'
Usage: bash docker/compose.sh [options] ACTION [Compose arguments]
  --hardware generic|gpu|npu     Control hardware (default: generic)
  --mode monitoring|cpu|npu|full (default: monitoring)
  --project NAME                 Compose project (default: catmonitor)
  --generated DIR                Existing generator output; required for Stress
  --config FILE                  Optional monitoring-only CATMonitor YAML
  --public-host HOST             Host/IP to display in URLs (not a port mapping)
  --backend auto|v2|legacy        Auto prefers Compose v2; explicit legacy is supported
  --overlay FILE                 Append an administrator override; repeatable
Actions: config, up, ps, logs, exec, stop, start, restart, down, urls

Run the existing generate_stress_deployment.sh first for Stress modes.
Images must already exist or be pullable. No image build or runtime selection
is performed here. Web keeps its :19322/+1 behavior; urls reads actual logs.
Bridge-mode URLs also use Docker's actual published ports, never the container
listener as a guessed host port. --project alone does not isolate fixed names.
Compose v2 is preferred. Legacy docker-compose uses a temporary format-2.4
view of the same files, requiring Python 3 + PyYAML; originals are not edited.
Legacy NPU startup uses the read-only repository Python PID-1 helper instead
of host docker-init; keep the source directory available while deployed.
EOF
}
while [ "$#" -gt 0 ]; do
    case "$1" in
        --hardware|--mode|--project|--generated|--config|--public-host|--backend|--overlay)
            [ "$#" -ge 2 ] && [ -n "$2" ] || die "missing value for $1"
            case "$1" in
                --hardware) HARDWARE=$2 ;; --mode) MODE=$2 ;; --project) PROJECT=$2 ;;
                --generated) GENERATED=$2 ;; --config) CONFIG=$2 ;; --public-host) PUBLIC_HOST=$2 ;;
                --backend) BACKEND=$2 ;; --overlay) OVERLAYS+=("$2") ;;
            esac
            shift 2 ;;
        -h|--help) usage; exit 0 ;;
        --*) die "unknown option: $1" ;;
        *) break ;;
    esac
done
[ "$#" -gt 0 ] || { usage; exit 2; }
ACTION=$1; shift
case "$ACTION" in config|up|ps|logs|exec|stop|start|restart|down|urls) ;; *) die "unsupported action: $ACTION" ;; esac
case "$HARDWARE" in generic|gpu|npu) ;; *) die "invalid hardware" ;; esac
case "$MODE" in monitoring|cpu) ;; npu|full) [ "$HARDWARE" = npu ] || die "NPU Stress requires --hardware npu" ;; *) die "invalid mode" ;; esac
[[ "$PROJECT" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die "invalid Compose project name"
case "$BACKEND" in auto|v2|legacy) ;; *) die "invalid Compose backend" ;; esac
# The requested mode, not a stale parent-shell profile, owns this invocation.
unset COMPOSE_PROFILES
[ -z "$PUBLIC_HOST" ] || [[ "$PUBLIC_HOST" =~ ^[a-zA-Z0-9._:-]+$ ]] || die "--public-host must be a hostname or IP, not a URL"
command -v docker >/dev/null || die "Docker CLI is required"

compose=(docker compose)
legacy=false
if [ "$BACKEND" = legacy ] || ! docker compose version >/dev/null 2>&1; then
    [ "$BACKEND" != v2 ] || die "Compose v2 was requested but is unavailable"
    command -v docker-compose >/dev/null || die "Compose not found; install Compose or use the documented manual docker run path"
    docker-compose version >/dev/null 2>&1 || die "docker-compose is not usable"
    compose=(docker-compose)
    legacy=true
fi
files=("$ROOT/docker/docker-compose.yml")
[ "$HARDWARE" = generic ] || files+=("$ROOT/docker/docker-compose.$HARDWARE.yml")
profiles=()
if [ "$MODE" != monitoring ]; then
    [ -z "$CONFIG" ] || die "Stress already mounts its generated YAML; do not add --config"
    [ -n "$GENERATED" ] && [ -d "$GENERATED" ] || die "--generated must name an existing generator output directory"
    GENERATED=$(cd -- "$GENERATED" && pwd)
    command -v python3 >/dev/null || die "python3 is required to check generated Stress profile selection"
    python3 "$ROOT/docker/compose_support.py" check-generated "$MODE" "$GENERATED"
    files+=("$ROOT/docker/docker-compose.stress.yml" "$GENERATED/docker-compose.stress.generated.yml")
    case "$MODE" in cpu) profiles=(stress-cpu) ;; npu) profiles=(stress-npu) ;; full) profiles=(stress-cpu stress-npu) ;; esac
elif [ -n "$GENERATED" ]; then
    die "monitoring mode must not include generated Stress configuration"
fi
if [ -n "$CONFIG" ]; then
    [ -f "$CONFIG" ] || die "--config must be an existing file"
    CONFIG=$(cd -- "$(dirname -- "$CONFIG")" && pwd)/$(basename -- "$CONFIG")
    export CATMONITOR_CONFIG=$CONFIG
    files+=("$ROOT/docker/docker-compose.config.yml")
fi
for overlay in "${OVERLAYS[@]}"; do
    [ -f "$overlay" ] || die "--overlay must be an existing file"
    files+=("$(cd -- "$(dirname -- "$overlay")" && pwd)/$(basename -- "$overlay")")
done

TEMP=
trap '[ -z "$TEMP" ] || rm -f -- "$TEMP"/*.json "$TEMP"/*.yml; [ -z "$TEMP" ] || rmdir -- "$TEMP"' EXIT
if [ "$legacy" = true ]; then
    command -v python3 >/dev/null || die "legacy Compose adapter requires python3 + PyYAML (or use Compose v2)"
    TEMP=$(mktemp -d)
    python3 "$ROOT/docker/compose_support.py" legacy "$MODE" "$TEMP" "${files[@]}"
    files=("$TEMP"/*.json)
fi
args=(--project-directory "$ROOT" -p "$PROJECT")
for file in "${files[@]}"; do args+=(-f "$file"); done
if [ "$legacy" != true ]; then
    for profile in "${profiles[@]}"; do args+=(--profile "$profile"); done
fi
printf 'COMPOSE_BACKEND=%s PROJECT=%s HARDWARE=%s MODE=%s\n' "${compose[*]}" "$PROJECT" "$HARDWARE" "$MODE" >&2

urls() {
    local service id started logs network published
    command -v python3 >/dev/null || die "python3 is required to report actual listening URLs"
    for service in web dfee; do
        id=$("${compose[@]}" "${args[@]}" ps -q "$service")
        [ -n "$id" ] && [[ "$id" != *$'\n'* ]] || die "no unique container for $service"
        [ "$(docker inspect --format '{{.State.Running}}' "$id")" = true ] || die "$service is not running"
        started=$(docker inspect --format '{{.State.StartedAt}}' "$id")
        # Ignore logs from a prior start, even when the same container restarted.
        logs=$(docker logs --since "$started" "$id" 2>&1) || die "cannot read $service startup log"
        network=$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$id")
        published=$(docker inspect --format '{{json .NetworkSettings.Ports}}' "$id")
        printf '%s\n' "$logs" | python3 "$ROOT/docker/compose_support.py" urls "$service" "$PUBLIC_HOST" "$network" "$published"
    done
}

case "$ACTION" in
    urls) [ "$#" -eq 0 ] || die "urls takes no Compose arguments"; urls ;;
    up)
        # Validate the complete resolved contract before creating any container.
        if [ "$MODE" = monitoring ]; then
            # Preserve the original Compose-v2 monitoring entry: Python is not
            # a new prerequisite merely to start the three monitoring services.
            "${compose[@]}" "${args[@]}" config >/dev/null
            "${compose[@]}" "${args[@]}" up -d "$@"
            exit
        fi
        command -v python3 >/dev/null || die "python3 is required for pre-start contract checks"
        [ -n "$TEMP" ] || TEMP=$(mktemp -d)
        if [ "$legacy" = true ]; then
            "${compose[@]}" "${args[@]}" config >"$TEMP/resolved.yml"
            resolved="$TEMP/resolved.yml"
        else
            "${compose[@]}" "${args[@]}" config --format json >"$TEMP/resolved.json"
            resolved="$TEMP/resolved.json"
        fi
        python3 "$ROOT/docker/compose_support.py" validate "$MODE" "$GENERATED" "$resolved"
        if [ "$MODE" != monitoring ]; then
            while IFS= read -r name; do
                if owner=$(docker container inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$name" 2>/dev/null); then
                    [ "$owner" = "$PROJECT" ] || die "container $name belongs to another project; regenerate with distinct workload names"
                fi
            done < <(python3 "$ROOT/docker/compose_support.py" workload-names "$MODE" "$GENERATED")
        fi
        if [ "$MODE" = npu ] || [ "$MODE" = full ]; then
            required_runtime=$(python3 "$ROOT/docker/compose_support.py" runtime "$resolved")
            docker info --format '{{json .Runtimes}}' | python3 "$ROOT/docker/compose_support.py" check-runtime "$required_runtime"
        fi
        "${compose[@]}" "${args[@]}" up -d "$@"
        printf 'Read the actual Web/DFeE listener with the same options and action: urls\n' >&2 ;;
    *) "${compose[@]}" "${args[@]}" "$ACTION" "$@" ;;
esac
