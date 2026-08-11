ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG SOURCE_SHA256
ARG PATCHED_SOURCE_SHA256
ARG COMPAT_PROFILE=none
ARG SOURCE_ORIGIN
ARG UPSTREAM_REPOSITORY
ARG UPSTREAM_REVISION
ARG ASCEND_ENV_SCRIPT
ARG BUILD_VALIDATION_NONCE

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

WORKDIR /opt/catmonitor/npuburn-source
COPY source/ /opt/catmonitor/npuburn-source/
COPY ascend_env.sh /usr/local/libexec/catmonitor/ascend_env.sh
COPY entrypoint.sh /usr/local/bin/catmonitor-npu-burn

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

# C. Always replace a same-version package from the base image with the wheel
# built from CATMonitor's audited source. Installation is local and offline.
RUN set -euo pipefail; \
    set -- build/dist/ascend_npu_burn-*.whl; \
    [ "$#" -eq 1 ] && [ -f "$1" ]; \
    python3 -m pip install \
        --no-index \
        --no-cache-dir \
        --no-deps \
        --force-reinstall \
        "$1"

# D. Re-run read-only preflight and package validation in a deliberately
# uncached layer so the host builder can record provenance from build output.
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
    package_version=$(python3 -c 'import importlib.metadata as metadata; print(metadata.version("ascend-npu-burn"))'); \
    package_file=$(python3 -c 'import ascend_npu_burn; print(ascend_npu_burn.__file__)'); \
    python3 -c 'import ascend_npu_burn.custom_ops.custom_ops_lib'; \
    printf 'CATMONITOR_WHEEL_FILENAME=%s\n' "$wheel_filename"; \
    printf 'CATMONITOR_WHEEL_SHA256=%s\n' "$wheel_sha256"; \
    printf 'CATMONITOR_PACKAGE_VERSION=%s\n' "$package_version"; \
    printf 'CATMONITOR_PACKAGE_FILE=%s\n' "$package_file"; \
    printf 'CATMONITOR_CUSTOM_OPS_IMPORT=PASS\n'; \
    /usr/local/bin/catmonitor-npu-burn --version; \
    install -d -m 0750 "$HOME/.ascend_npu_burn/output"; \
    install -d -m 0755 /usr/share/licenses/ascend-npu-burn; \
    install -m 0644 LICENSE.md /usr/share/licenses/ascend-npu-burn/LICENSE.md

ENTRYPOINT ["/usr/local/bin/catmonitor-npu-burn"]
CMD ["__serve__"]
