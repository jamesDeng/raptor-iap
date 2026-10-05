FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway
FROM alpine:3.23
RUN apk add --no-cache ca-certificates git && addgroup -g 10001 app && adduser -D -u 10001 -G app app
COPY --from=build /out/ /usr/local/bin/
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/gateway"]
