# Multi-stage: build the Go binary, then put it on the same MapServer image
# that serves maps, so mapserv/gdalinfo match the renderer (PLAN.md §2).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
# Dependencies exactly as pinned in go.sum, checked against their hashes.
RUN go mod download && go mod verify
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/msapi ./cmd/msapi

FROM camptocamp/mapserver:8.6-gdal3.13
# Fonts for labels; msapi copies them into the shared mapfiles volume.
RUN apt-get update \
 && apt-get install -y --no-install-recommends fonts-dejavu-core \
 && apt-get upgrade -y --no-install-recommends \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/msapi /usr/local/bin/msapi
COPY docker/mapserver-validate.conf /etc/msapi/mapserver-validate.conf
ENV MSAPI_LISTEN=:8081 \
    MSAPI_DATA_DIR=/data \
    MSAPI_MAPFILE_DIR=/etc/mapserver \
    MSAPI_VALIDATE_CONFIG=/etc/msapi/mapserver-validate.conf \
    PGSERVICEFILE=/etc/mapserver/secrets/pg_service.conf
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/usr/local/bin/msapi", "healthcheck"]
# The base image starts Apache; this container runs only the API.
ENTRYPOINT ["/usr/local/bin/msapi"]
CMD []
