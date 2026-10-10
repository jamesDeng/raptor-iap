# Build with --build-context agent-harness=../agent-harness.
# The named context contains source only; never a credential/state directory.
FROM agent-harness AS harness
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sandbox-compat ./cmd/sandbox-compat
FROM alpine:3.23
RUN apk add --no-cache ca-certificates git && addgroup -g 10001 app && adduser -D -u 10001 -G app app
COPY --from=build /out/ /usr/local/bin/
COPY --from=harness /package.json /package-lock.json /archive.py /application-context.mjs /app-question.mjs /checkpoint.mjs /pi-adapter.mjs /runner.mjs /progress.mjs /live-contract.mjs /live-runner.mjs /live-question.mjs /mcp-runtime.mjs /operation-profile.mjs /replacement-evidence.mjs /operation-session.mjs /skills-loader.mjs /live-operation.mjs /approval-pause.mjs /live-replacement.mjs /skills-bundle.mjs /client-targets.mjs /replacement-trace.mjs /pgcat-metrics.mjs /replacement-completion.mjs /observation-tools.mjs /observation-extension.mjs /observation-config.mjs /conversation.mjs /opt/raptor-harness/
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/gateway"]
