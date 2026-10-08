# Build context: components/test-client. Central publishing resolves immutable image digest.
FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /client ./cmd/client
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /client /client
EXPOSE 9090
ENTRYPOINT ["/client"]
