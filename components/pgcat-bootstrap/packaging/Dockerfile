# Build context is the qualification directory produced by the Linux workflow.
FROM debian:bookworm-slim
RUN apt-get update -qq && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /opt/raptor-pgcat /var/lib/raptor-pgcat /etc/raptor-pgcat \
    && chown 65532:65532 /var/lib/raptor-pgcat && chmod 0700 /var/lib/raptor-pgcat
COPY --chmod=0755 bootstrap pgcat /opt/raptor-pgcat/
COPY bootstrap-revision.txt pgcat-revision.txt /opt/raptor-pgcat/
USER 65532:65532
EXPOSE 6432 9930
ENTRYPOINT ["/opt/raptor-pgcat/bootstrap", "--config", "/etc/raptor-pgcat/bootstrap.json"]
