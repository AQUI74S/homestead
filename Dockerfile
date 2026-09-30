# syntax=docker/dockerfile:1

# --- build ---
# Runs on the build machine's platform and cross-compiles for the target
# platform, so multi-arch images build fast without emulation.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/homestead ./cmd/homestead

# --- runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="homestead" \
      org.opencontainers.image.description="Self-hosted household budget and property management with automatic bank sync" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/homestead /homestead
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/homestead"]
