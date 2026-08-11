ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS npuburn_builder

ARG ASCEND_ENV_SCRIPT

ENV HOME=/opt/catmonitor/npuburn-home \
    ASCEND_ENV_SCRIPT=${ASCEND_ENV_SCRIPT}

SHELL ["/bin/bash", "-c"]

WORKDIR /opt/catmonitor/npuburn-source
COPY source/ /opt/catmonitor/npuburn-source/
COPY ascend_env.sh /usr/local/libexec/catmonitor/ascend_env.sh
COPY entrypoint.sh /usr/local/bin/catmonitor-npu-burn

# Optional host driver libraries are staged only in this disposable builder
# stage. The final runtime image starts from BASE_IMAGE again and cannot retain
# them. An empty directory preserves the normal self-contained A3 path.
COPY build-driver-lib64/ /usr/local/Ascend/driver/lib64/

# Prepare immutable CATMonitor helpers.
RUN set -euo pipefail; \
    chmod 0755 /usr/local/bin/catmonitor-npu-burn; \
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
# The explicit device count makes --version deterministic in containers whose
# lscpu output hides socket/NUMA data; it is not persisted as a runtime default.
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
    printf 'CATMONITOR_WHEEL_FILENAME=%s\n' "$wheel_filename"; \
    printf 'CATMONITOR_WHEEL_SHA256=%s\n' "$wheel_sha256"; \
    printf 'CATMONITOR_PACKAGE_VERSION=%s\n' "$package_version"; \
    printf 'CATMONITOR_PACKAGE_FILE=%s\n' "$package_file"; \
    printf 'CATMONITOR_CUSTOM_OPS_IMPORT=PASS\n'; \
    CATMONITOR_NPU_DEVICE_COUNT=1 /usr/local/bin/catmonitor-npu-burn --version


FROM ${BASE_IMAGE} AS npuburn_runtime

ARG SOURCE_SHA256
ARG PATCHED_SOURCE_SHA256
ARG COMPAT_PROFILE=none
ARG SOURCE_ORIGIN
ARG UPSTREAM_REPOSITORY
ARG UPSTREAM_REVISION
ARG ASCEND_ENV_SCRIPT

LABEL org.opencontainers.image.title="CATMonitor Ascend NPU Burn runtime" \
      org.opencontainers.image.licenses="MulanPSL-2.0" \
      io.catmonitor.npu-burn.source-sha256="${SOURCE_SHA256}" \
      io.catmonitor.npu-burn.patched-source-sha256="${PATCHED_SOURCE_SHA256}" \
      io.catmonitor.npu-burn.compat-profile="${COMPAT_PROFILE}" \
      io.catmonitor.npu-burn.source-origin="${SOURCE_ORIGIN}" \
      io.catmonitor.npu-burn.upstream-repository="${UPSTREAM_REPOSITORY}" \
      io.catmonitor.npu-burn.upstream-revision="${UPSTREAM_REVISION}"

ENV HOME=/opt/catmonitor/npuburn-home \
    ASCEND_ENV_SCRIPT=${ASCEND_ENV_SCRIPT}

SHELL ["/bin/bash", "-c"]

COPY ascend_env.sh /usr/local/libexec/catmonitor/ascend_env.sh
COPY entrypoint.sh /usr/local/bin/catmonitor-npu-burn
COPY --from=npuburn_builder /opt/catmonitor/npuburn-source/build/dist/ /opt/catmonitor/npuburn-wheel/
COPY --from=npuburn_builder /opt/catmonitor/npuburn-source/LICENSE.md /usr/share/licenses/ascend-npu-burn/LICENSE.md

# E. Install the exact builder-validated wheel into the clean runtime stage.
# Host driver libraries from the optional builder input are not copied here.
RUN set -euo pipefail; \
    chmod 0755 /usr/local/bin/catmonitor-npu-burn; \
    chmod 0644 /usr/local/libexec/catmonitor/ascend_env.sh; \
    set -- /opt/catmonitor/npuburn-wheel/ascend_npu_burn-*.whl; \
    [ "$#" -eq 1 ] && [ -f "$1" ]; \
    python3 -m pip install \
        --no-index \
        --no-cache-dir \
        --no-deps \
        --force-reinstall \
        "$1"; \
    install -d -m 0750 "$HOME/.ascend_npu_burn/output"

ENTRYPOINT ["/usr/local/bin/catmonitor-npu-burn"]
CMD ["__serve__"]
