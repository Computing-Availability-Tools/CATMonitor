ARG GO_IMAGE=golang:1.23-bookworm
ARG BUILDER_BASE_IMAGE
ARG RUNTIME_BASE_IMAGE

FROM ${GO_IMAGE} AS stress_exec_builder
WORKDIR /src
COPY go.mod go.sum ./
COPY features/stress/workloadapi ./features/stress/workloadapi
COPY features/stress/resultparse ./features/stress/resultparse
COPY features/stress/workloadplugin ./features/stress/workloadplugin
COPY features/stress/cmd/workload-exec ./features/stress/cmd/workload-exec
ENV CGO_ENABLED=0
RUN go build -trimpath -o /out/catmonitor-stress-exec ./features/stress/cmd/workload-exec

FROM ${BUILDER_BASE_IMAGE} AS npuburn_builder

ARG BUILDER_ASCEND_ENV_SCRIPT

ENV HOME=/opt/catmonitor/npuburn-home \
    ASCEND_ENV_SCRIPT=${BUILDER_ASCEND_ENV_SCRIPT}

SHELL ["/bin/bash", "-c"]

WORKDIR /opt/catmonitor/npuburn-source
COPY source/ /opt/catmonitor/npuburn-source/
COPY ascend_env.sh /usr/local/libexec/catmonitor/ascend_env.sh
COPY validate_entrypoint.sh /usr/local/libexec/catmonitor/validate_entrypoint.sh
COPY validate_runtime_abi.py /usr/local/libexec/catmonitor/validate_runtime_abi.py
COPY entrypoint.sh /usr/local/bin/catmonitor-npu-burn

# Optional host driver libraries are staged only in this disposable builder
# stage. The final runtime image starts from RUNTIME_BASE_IMAGE and cannot
# retain them. An empty directory preserves the normal self-contained path.
COPY build-driver-lib64/ /usr/local/Ascend/driver/lib64/

# Prepare immutable CATMonitor helpers.
RUN set -euo pipefail; \
    chmod 0755 /usr/local/bin/catmonitor-npu-burn; \
    chmod 0755 /usr/local/libexec/catmonitor/validate_entrypoint.sh; \
    chmod 0644 /usr/local/libexec/catmonitor/ascend_env.sh

# A. Validate the build-capable Ascend software stack.
RUN set -euo pipefail; \
    source /usr/local/libexec/catmonitor/ascend_env.sh; \
    catmonitor_source_ascend_env; \
    catmonitor_ascend_build_preflight

# B. Build exactly one wheel. This expensive native compilation layer remains
# cacheable when only final package validation changes.
RUN set -euo pipefail; \
    source /usr/local/libexec/catmonitor/ascend_env.sh; \
    catmonitor_source_ascend_env; \
    bash build/build.sh; \
    set -- build/dist/ascend_npu_burn-*.whl; \
    [ "$#" -eq 1 ] && [ -f "$1" ]

# C. Replace any same-version package in the builder with the audited wheel.
# Installation is local and offline.
RUN set -euo pipefail; \
    set -- build/dist/ascend_npu_burn-*.whl; \
    [ "$#" -eq 1 ] && [ -f "$1" ]; \
    python3 -m pip install \
        --no-index \
        --no-cache-dir \
        --no-deps \
        --force-reinstall \
        "$1"

# D. Re-run read-only preflight and validate the wheel in an uncached layer.
# Build validation must not launch the runtime CLI: importing its command module
# performs NUMA discovery, which requires runtime topology that a Docker build
# environment is not required to expose.
ARG BUILD_VALIDATION_NONCE
RUN set -euo pipefail; \
    : "${BUILD_VALIDATION_NONCE:?build validation nonce is required}"; \
    source /usr/local/libexec/catmonitor/ascend_env.sh; \
    catmonitor_source_ascend_env; \
    catmonitor_ascend_build_preflight; \
    set -- build/dist/ascend_npu_burn-*.whl; \
    [ "$#" -eq 1 ] && [ -f "$1" ]; \
    wheel_filename=${1##*/}; \
    wheel_sha256=$(sha256sum "$1"); \
    wheel_sha256=${wheel_sha256%% *}; \
    cd /tmp; \
    package_version=$(python3 -c 'import importlib.metadata as metadata; print(metadata.version("ascend-npu-burn"))'); \
    package_file=$(python3 -c 'import ascend_npu_burn; print(ascend_npu_burn.__file__)'); \
    python3 -c 'import ascend_npu_burn.custom_ops.custom_ops_lib'; \
    /usr/local/libexec/catmonitor/validate_entrypoint.sh /usr/local/bin/catmonitor-npu-burn; \
    printf 'CATMONITOR_WHEEL_FILENAME=%s\n' "$wheel_filename"; \
    printf 'CATMONITOR_WHEEL_SHA256=%s\n' "$wheel_sha256"; \
    printf 'CATMONITOR_PACKAGE_VERSION=%s\n' "$package_version"; \
    printf 'CATMONITOR_PACKAGE_FILE=%s\n' "$package_file"; \
    printf 'CATMONITOR_CUSTOM_OPS_IMPORT=PASS\n'; \
    printf 'CATMONITOR_ENTRYPOINT_EXECUTABLE=PASS\n'; \
    install -d -m 0755 /opt/catmonitor/build-compat; \
    printf 'CATMONITOR_BUILDER_ASCEND_ENV_SCRIPT=%s\n' "$CATMONITOR_ASCEND_ENV_SCRIPT_SELECTED"; \
    python3 /usr/local/libexec/catmonitor/validate_runtime_abi.py capture \
        --cann-version "$CATMONITOR_CANN_VERSION" \
        --output /opt/catmonitor/build-compat/builder-abi.json


# Prepare the installed overlay with the builder's pip. This stage is
# disposable, so a minimal runtime base does not need pip or build tooling.
FROM npuburn_builder AS npuburn_runtime_package

SHELL ["/bin/bash", "-c"]

# Install into an isolated overlay. The final image copies only installed
# package files and metadata, not the wheel archive or builder toolchain.
RUN set -euo pipefail; \
    set -- /opt/catmonitor/npuburn-source/build/dist/ascend_npu_burn-*.whl; \
    [ "$#" -eq 1 ] && [ -f "$1" ]; \
    python3 -m pip install \
        --no-index \
        --no-cache-dir \
        --no-deps \
        --target /opt/catmonitor/npuburn-python \
        "$1"; \
    rm -rf /opt/catmonitor/npuburn-source/build/dist


FROM ${RUNTIME_BASE_IMAGE} AS npuburn_runtime

ARG SOURCE_SHA256
ARG PATCHED_SOURCE_SHA256
ARG COMPAT_PROFILE=none
ARG SOURCE_ORIGIN
ARG UPSTREAM_REPOSITORY
ARG UPSTREAM_REVISION
ARG RUNTIME_ASCEND_ENV_SCRIPT
ARG BUILD_VALIDATION_NONCE
ARG PCIUTILS_ONLINE_INSTALL=false

LABEL org.opencontainers.image.title="CATMonitor Ascend NPU Burn runtime" \
      org.opencontainers.image.licenses="MulanPSL-2.0" \
      io.catmonitor.npu-burn.source-sha256="${SOURCE_SHA256}" \
      io.catmonitor.npu-burn.patched-source-sha256="${PATCHED_SOURCE_SHA256}" \
      io.catmonitor.npu-burn.compat-profile="${COMPAT_PROFILE}" \
      io.catmonitor.npu-burn.source-origin="${SOURCE_ORIGIN}" \
      io.catmonitor.npu-burn.upstream-repository="${UPSTREAM_REPOSITORY}" \
      io.catmonitor.npu-burn.upstream-revision="${UPSTREAM_REVISION}"

ENV HOME=/opt/catmonitor/npuburn-home \
    ASCEND_ENV_SCRIPT=${RUNTIME_ASCEND_ENV_SCRIPT} \
    PYTHONPATH=/opt/catmonitor/npuburn-python:${PYTHONPATH} \
    CATMONITOR_STRESS_BENCHMARKS=npu_burn \
    CATMONITOR_STRESS_STATE_ROOT=/var/lib/catmonitor/stress \
    NPU_BURN_BACKEND=native \
    NPU_BURN_EXECUTABLE=/usr/local/bin/catmonitor-npu-burn \
    NPU_BURN_PREFLIGHT_EXECUTABLE=/usr/local/bin/catmonitor-npu-burn-preflight \
    NPU_BURN_OUTPUT_DIR=/opt/catmonitor/npuburn-home/.ascend_npu_burn/output \
    NPU_BURN_LOG_DIR=/opt/catmonitor/npuburn-home/.ascend_npu_burn/log \
    XDG_CACHE_HOME=/opt/catmonitor/npuburn-home/.cache \
    ASCEND_PROCESS_LOG_PATH=/opt/catmonitor/npuburn-home/.ascend_npu_burn/log/ascend \
    NPU_BURN_INTERNAL_TIMEOUT_SECONDS=300

SHELL ["/bin/bash", "-c"]

COPY ascend_env.sh /usr/local/libexec/catmonitor/ascend_env.sh
COPY validate_runtime_abi.py /usr/local/libexec/catmonitor/validate_runtime_abi.py
COPY runtime_preflight.sh /usr/local/bin/catmonitor-npu-burn-preflight
COPY entrypoint.sh /usr/local/bin/catmonitor-npu-burn
COPY --from=stress_exec_builder /out/catmonitor-stress-exec /usr/local/bin/catmonitor-stress-exec
COPY --from=npuburn_builder /opt/catmonitor/build-compat/builder-abi.json /usr/local/share/catmonitor/builder-abi.json
COPY --from=npuburn_runtime_package /opt/catmonitor/npuburn-python/ /opt/catmonitor/npuburn-python/
COPY --from=npuburn_builder /opt/catmonitor/npuburn-source/LICENSE.md /usr/share/licenses/ascend-npu-burn/LICENSE.md
COPY pciutils-packages/ /opt/catmonitor/pciutils-packages/
COPY runtime-packages.txt /opt/catmonitor/runtime-packages.txt

# E. Provide the upstream runtime topology dependency. Offline packages staged
# by the outer builder take precedence. Online package-manager access is used
# only when the administrator explicitly enables a Docker build network.
# NPU Burn silently falls back to eight devices without lspci, which is wrong
# on 16-die A3 nodes.
RUN set -euo pipefail; \
    mapfile -t runtime_packages < <(awk 'NF && $1 !~ /^#/ { print $1 }' /opt/catmonitor/runtime-packages.txt); \
    [ "${#runtime_packages[@]}" -gt 0 ]; \
    if command -v lspci >/dev/null 2>&1; then \
        pciutils_source=base-image; \
    elif set -- /opt/catmonitor/pciutils-packages/*.rpm; [ -f "$1" ]; then \
        command -v rpm >/dev/null 2>&1 || { \
            printf 'ERROR: offline RPM packages were supplied but rpm is unavailable\n' >&2; \
            exit 1; \
        }; \
        rpm -Uvh --replacepkgs "$@"; \
        pciutils_source=offline-rpm; \
    elif set -- /opt/catmonitor/pciutils-packages/*.deb; [ -f "$1" ]; then \
        command -v dpkg >/dev/null 2>&1 || { \
            printf 'ERROR: offline DEB packages were supplied but dpkg is unavailable\n' >&2; \
            exit 1; \
        }; \
        dpkg -i "$@"; \
        pciutils_source=offline-deb; \
    elif [ "$PCIUTILS_ONLINE_INSTALL" != true ]; then \
        printf 'ERROR: lspci is absent; provide compatible offline pciutils packages or explicitly enable a build network\n' >&2; \
        exit 1; \
    elif command -v dnf >/dev/null 2>&1; then \
        dnf install -y "${runtime_packages[@]}" || { \
            printf 'ERROR: online runtime package installation failed; configure a temporary proxy or provide offline packages\n' >&2; \
            exit 1; \
        }; \
        dnf clean all; \
        pciutils_source=online-dnf; \
    elif command -v yum >/dev/null 2>&1; then \
        yum install -y "${runtime_packages[@]}" || { \
            printf 'ERROR: online runtime package installation failed; configure a temporary proxy or provide offline packages\n' >&2; \
            exit 1; \
        }; \
        yum clean all; \
        pciutils_source=online-yum; \
    elif command -v apt-get >/dev/null 2>&1; then \
        export DEBIAN_FRONTEND=noninteractive; \
        apt-get update || { \
            printf 'ERROR: online runtime package index update failed; configure a temporary proxy or provide offline packages\n' >&2; \
            exit 1; \
        }; \
        apt-get install -y --no-install-recommends "${runtime_packages[@]}" || { \
            printf 'ERROR: online runtime package installation failed; configure a temporary proxy or provide offline packages\n' >&2; \
            exit 1; \
        }; \
        rm -rf /var/lib/apt/lists/*; \
        pciutils_source=online-apt; \
    elif command -v zypper >/dev/null 2>&1; then \
        zypper --non-interactive install --no-recommends "${runtime_packages[@]}" || { \
            printf 'ERROR: online runtime package installation failed; configure a temporary proxy or provide offline packages\n' >&2; \
            exit 1; \
        }; \
        zypper clean --all; \
        pciutils_source=online-zypper; \
    else \
        printf 'ERROR: pciutils is absent and the base image has no supported package manager\n' >&2; \
        exit 1; \
    fi; \
    command -v lspci >/dev/null; \
    install -d -m 0755 /usr/local/share/catmonitor; \
    printf '%s\n' "$pciutils_source" > /usr/local/share/catmonitor/pciutils-source; \
    rm -rf /opt/catmonitor/pciutils-packages /opt/catmonitor/runtime-packages.txt

# F. Validate the slim runtime base against the builder ABI. CANN remains in
# the runtime image; only host driver libraries/devices are mounted later.
# Host driver libraries from the optional builder input are never copied here.
RUN set -euo pipefail; \
    chmod 0755 /usr/local/bin/catmonitor-npu-burn; \
    chmod 0755 /usr/local/bin/catmonitor-npu-burn-preflight; \
    chmod 0755 /usr/local/bin/catmonitor-stress-exec; \
    chmod 0644 /usr/local/libexec/catmonitor/ascend_env.sh; \
    chmod 0644 /usr/local/libexec/catmonitor/validate_runtime_abi.py; \
    printf '#!/usr/bin/env bash\nexec python3 -m ascend_npu_burn.npu_burn "$@"\n' \
        > /usr/local/bin/npu-burn; \
    chmod 0755 /usr/local/bin/npu-burn; \
    source /usr/local/libexec/catmonitor/ascend_env.sh; \
    catmonitor_source_ascend_env; \
    printf 'CATMONITOR_RUNTIME_ASCEND_ENV_SCRIPT=%s\n' "$CATMONITOR_ASCEND_ENV_SCRIPT_SELECTED"; \
    python3 /usr/local/libexec/catmonitor/validate_runtime_abi.py validate \
        --expected /usr/local/share/catmonitor/builder-abi.json \
        --cann-version "$CATMONITOR_CANN_VERSION"; \
    runtime_package_version=$(python3 -c 'import importlib.metadata as metadata; print(metadata.version("ascend-npu-burn"))'); \
    printf 'CATMONITOR_RUNTIME_NPUBURN_PACKAGE_VERSION=%s\n' "$runtime_package_version"; \
    install -d -m 0750 \
        "$HOME/.ascend_npu_burn/output" \
        "$HOME/.ascend_npu_burn/log" \
        "$HOME/.ascend_npu_burn/log/ascend" \
        "$HOME/.tvm_test_data" \
        "$HOME/.cache"

# G. Revalidate the final image without importing the runtime command module or
# touching NPU devices. The nonce prevents a cached layer from hiding markers
# that the outer builder records in its manifest.
RUN set -euo pipefail; \
    : "${BUILD_VALIDATION_NONCE:?build validation nonce is required}"; \
    lspci_path=$(command -v lspci); \
    lspci_version=$("$lspci_path" --version 2>&1); \
    [ -n "$lspci_version" ]; \
    pciutils_source=$(cat /usr/local/share/catmonitor/pciutils-source); \
    [ -n "$pciutils_source" ]; \
    printf 'CATMONITOR_RUNTIME_PCIUTILS=PASS\n'; \
    printf 'CATMONITOR_PCIUTILS_SOURCE=%s\n' "$pciutils_source"; \
    printf 'CATMONITOR_LSPCI_PATH=%s\n' "$lspci_path"; \
    printf 'CATMONITOR_LSPCI_VERSION=%s\n' "${lspci_version%%$'\n'*}"

ENTRYPOINT ["/usr/local/bin/catmonitor-npu-burn"]
CMD ["__serve__"]
