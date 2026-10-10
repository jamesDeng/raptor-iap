FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backend ./cmd/backend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/frontend ./cmd/frontend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/open-api ./cmd/open-api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/admin ./cmd/admin
FROM node:22.23.3-alpine AS model-helper
WORKDIR /opt/raptor-model-auth
COPY --from=agent-harness /package.json /package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts --no-audit --no-fund
COPY --from=agent-harness /model-auth-helper.mjs ./
FROM alpine:3.23
RUN apk add --no-cache ca-certificates git libstdc++ && addgroup -g 10001 app && adduser -D -u 10001 -G app app
COPY --from=build /out/ /usr/local/bin/
COPY --from=model-helper /usr/local/bin/node /usr/local/bin/node
COPY --from=model-helper /opt/raptor-model-auth /opt/raptor-model-auth
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/backend"]
