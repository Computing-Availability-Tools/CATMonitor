ARG GO_IMAGE=golang:1.23-bookworm
ARG BUILD_IMAGE=debian:bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim

FROM ${GO_IMAGE} AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
COPY features/stress/runnerapi ./features/stress/runnerapi
COPY features/stress/cmd/cpu-runner ./features/stress/cmd/cpu-runner
ARG GOPROXY
ARG GOSUMDB
ARG GOPRIVATE
ARG GONOSUMDB
ENV CGO_ENABLED=0
RUN go build -trimpath -o /out/catmonitor-stress-cpu-runner ./features/stress/cmd/cpu-runner

FROM ${BUILD_IMAGE} AS benchmark-builder
ARG DEBIAN_MIRROR=""
RUN if [ -n "${DEBIAN_MIRROR}" ]; then \
      case "${DEBIAN_MIRROR}" in \
        http://?*|https://?*) ;; \
        *) echo "invalid DEBIAN_MIRROR" >&2; exit 2 ;; \
      esac; \
      authority="${DEBIAN_MIRROR#*://}"; \
      authority="${authority%/}"; \
      case "${authority}" in \
        ''|*/*|*@*|*\?*|*\#*|*[[:space:]]*) \
          echo "invalid DEBIAN_MIRROR root" >&2; exit 2 ;; \
      esac; \
      root="${DEBIAN_MIRROR%/}"; \
      for source_file in \
        /etc/apt/sources.list \
        /etc/apt/sources.list.d/*.list \
        /etc/apt/sources.list.d/*.sources; do \
        [ -f "${source_file}" ] || continue; \
        sed -i \
          -e "s#http://deb.debian.org/debian-security#${root}/debian-security#g" \
          -e "s#https://deb.debian.org/debian-security#${root}/debian-security#g" \
          -e "s#http://deb.debian.org/debian#${root}/debian#g" \
          -e "s#https://deb.debian.org/debian#${root}/debian#g" \
          "${source_file}"; \
      done; \
    fi \
    && apt-get update && apt-get install -y --no-install-recommends \
    bash \
    build-essential \
    file \
    gfortran \
    libopenblas-dev \
    libmpich-dev \
    mpich \
    numactl \
    && rm -rf /var/lib/apt/lists/*

COPY scripts/stress/build_cpu_benchmarks.sh /build/scripts/stress/build_cpu_benchmarks.sh
COPY scripts/stress/templates/Make.HPL.CATMonitor /build/scripts/stress/templates/Make.HPL.CATMonitor
COPY inputs /build/inputs

ARG BUILD_JOBS=16
ARG STREAM_ARRAY_SIZE=80000000
ARG STREAM_NTIMES=10
ARG STREAM_SMOKE_NUMA_POLICY=none
RUN bash /build/scripts/stress/build_cpu_benchmarks.sh \
    --stream-src /build/inputs/stream.c \
    --hpl-src /build/inputs/hpl.tar.gz \
    --hpl-dat /build/inputs/HPL.dat \
    --hpcg-src /build/inputs/hpcg.tar.gz \
    --hpcg-dat /build/inputs/hpcg.dat \
    --output-root /opt/catmonitor/stress/runtime \
    --build-root /build/work \
    --cc /usr/bin/gcc \
    --cxx /usr/bin/g++ \
    --mpicc /usr/bin/mpicc \
    --mpicxx /usr/bin/mpicxx \
    --mpirun /usr/bin/mpirun \
    --openblas-include /usr/include \
    --openblas-lib /usr/lib/$(dpkg-architecture -qDEB_HOST_MULTIARCH) \
    --jobs "$BUILD_JOBS" \
    --stream-array-size "$STREAM_ARRAY_SIZE" \
    --stream-ntimes "$STREAM_NTIMES" \
    --stream-smoke-numa-policy "$STREAM_SMOKE_NUMA_POLICY"

FROM ${RUNTIME_IMAGE}
ARG DEBIAN_MIRROR=""
RUN if [ -n "${DEBIAN_MIRROR}" ]; then \
      case "${DEBIAN_MIRROR}" in \
        http://?*|https://?*) ;; \
        *) echo "invalid DEBIAN_MIRROR" >&2; exit 2 ;; \
      esac; \
      authority="${DEBIAN_MIRROR#*://}"; \
      authority="${authority%/}"; \
      case "${authority}" in \
        ''|*/*|*@*|*\?*|*\#*|*[[:space:]]*) \
          echo "invalid DEBIAN_MIRROR root" >&2; exit 2 ;; \
      esac; \
      root="${DEBIAN_MIRROR%/}"; \
      for source_file in \
        /etc/apt/sources.list \
        /etc/apt/sources.list.d/*.list \
        /etc/apt/sources.list.d/*.sources; do \
        [ -f "${source_file}" ] || continue; \
        sed -i \
          -e "s#http://deb.debian.org/debian-security#${root}/debian-security#g" \
          -e "s#https://deb.debian.org/debian-security#${root}/debian-security#g" \
          -e "s#http://deb.debian.org/debian#${root}/debian#g" \
          -e "s#https://deb.debian.org/debian#${root}/debian#g" \
          "${source_file}"; \
      done; \
    fi \
    && apt-get update && apt-get install -y --no-install-recommends \
    bash \
    libgomp1 \
    libopenblas0 \
    mpich \
    numactl \
    util-linux \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 65532 catmonitor-stress \
    && useradd --system --uid 65532 --gid 65532 --home-dir /nonexistent \
       --shell /usr/sbin/nologin catmonitor-stress

COPY --from=go-builder /out/catmonitor-stress-cpu-runner /usr/local/bin/catmonitor-stress-cpu-runner
COPY --from=benchmark-builder /opt/catmonitor/stress/runtime /opt/catmonitor/stress/runtime
COPY --from=benchmark-builder /opt/catmonitor/stress/manifests /opt/catmonitor/stress/manifests
COPY docker/stress/cpu/entrypoint.sh /usr/local/bin/catmonitor-stress-cpu-entrypoint

RUN chmod 0755 \
      /usr/local/bin/catmonitor-stress-cpu-runner \
      /usr/local/bin/catmonitor-stress-cpu-entrypoint \
    && find /opt/catmonitor/stress/runtime -type d -exec chmod 0755 {} + \
    && find /opt/catmonitor/stress/runtime -type f -exec chmod go-w {} + \
    && command -v mpirun >/dev/null \
    && command -v numactl >/dev/null \
    && test -x /opt/catmonitor/stress/runtime/stream/stream_omp \
    && test -x /opt/catmonitor/stress/runtime/hpl/xhpl \
    && test -r /opt/catmonitor/stress/runtime/hpl/HPL.dat \
    && test -x /opt/catmonitor/stress/runtime/hpcg/xhpcg \
    && test -r /opt/catmonitor/stress/runtime/hpcg/hpcg.dat \
    && ! ldd /opt/catmonitor/stress/runtime/hpl/xhpl | grep -q 'not found' \
    && ! ldd /opt/catmonitor/stress/runtime/hpcg/xhpcg | grep -q 'not found'

ENTRYPOINT ["/usr/local/bin/catmonitor-stress-cpu-entrypoint"]
CMD ["/usr/local/bin/catmonitor-stress-cpu-runner", "-socket", "/run/catmonitor-stress/cpu-runner.sock", "-adapter", "/etc/catmonitor-stress/benchmark_check.sh"]
