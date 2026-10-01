# Official node:22-bookworm Linux AMD64 manifest, resolved 2026-10-01.
FROM node:22-bookworm@sha256:17b7fd60fd812617654c64b95f9b2dde94f103313073b672bc40fdad6dccbaa2

LABEL org.opencontainers.image.source="https://github.com/jamesDeng/raptor-iap"

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash git curl ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && npm install --global --ignore-scripts --no-audit --no-fund @earendil-works/pi-coding-agent@0.99.2 \
    && npm cache clean --force

# Keep the official base's root user and writable account files for sandbox init.
WORKDIR /workspace
CMD ["sleep", "infinity"]
