ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG SOURCE_SHA256
ARG PATCHED_SOURCE_SHA256
ARG COMPAT_PROFILE=none

LABEL org.opencontainers.image.title="CATMonitor Ascend NPU Burn runtime" \
      org.opencontainers.image.licenses="MulanPSL-2.0" \
      io.catmonitor.npu-burn.source-sha256="${SOURCE_SHA256}" \
      io.catmonitor.npu-burn.patched-source-sha256="${PATCHED_SOURCE_SHA256}" \
      io.catmonitor.npu-burn.compat-profile="${COMPAT_PROFILE}"

ENV HOME=/opt/catmonitor/npuburn-home

WORKDIR /opt/catmonitor/npuburn-source
COPY source/ /opt/catmonitor/npuburn-source/
COPY entrypoint.sh /usr/local/bin/catmonitor-npu-burn

RUN set -eux; \
    chmod 0755 /usr/local/bin/catmonitor-npu-burn; \
    if [ -r /usr/local/Ascend/ascend-toolkit/set_env.sh ]; then \
        . /usr/local/Ascend/ascend-toolkit/set_env.sh; \
    elif [ -r /usr/local/Ascend/ascend-toolkit/latest/bin/setenv.bash ]; then \
        . /usr/local/Ascend/ascend-toolkit/latest/bin/setenv.bash; \
    fi; \
    bash build/build.sh; \
    set -- build/dist/ascend_npu_burn-*.whl; \
    [ "$#" -eq 1 ] && [ -f "$1" ]; \
    python3 -m pip install --no-cache-dir --no-deps "$1"; \
    python3 -c 'import torch; import torch_npu; import ascend_npu_burn'; \
    /usr/local/bin/catmonitor-npu-burn --version; \
    install -d -m 0750 "$HOME/.ascend_npu_burn/output"; \
    install -d -m 0755 /usr/share/licenses/ascend-npu-burn; \
    install -m 0644 LICENSE.md /usr/share/licenses/ascend-npu-burn/LICENSE.md

ENTRYPOINT ["/usr/local/bin/catmonitor-npu-burn"]
CMD ["__serve__"]
