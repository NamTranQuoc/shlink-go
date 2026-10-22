# Builds the Go port of Shlink with the same runtime layout as the upstream image: working dir /etc/shlink, data in
# /etc/shlink/data, port 8080, the same entrypoint, and a `shlink` command on the PATH.
FROM golang:1.27.3-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shlink ./cmd/shlink

FROM alpine:3.22
ARG USER_ID=1001
# Upstream's default runtime; the entrypoint only starts the server for it
ENV SHLINK_RUNTIME=rr
ENV USER_ID=${USER_ID}
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /etc/shlink
# Same layout as upstream: bin/cli runs the console (and here the server too), `shlink` on the PATH links to it, and
# the installer is vendor/bin/shlink-installer (the same binary, dispatched by name)
COPY --from=builder --chown=${USER_ID} /out/shlink bin/cli
COPY --chown=${USER_ID} docker/docker-entrypoint.sh docker-entrypoint.sh
RUN mkdir -p data config/params vendor/bin && ln -s /etc/shlink/bin/cli vendor/bin/shlink-installer && chown -R ${USER_ID} /etc/shlink && \
    ln -s /etc/shlink/bin/cli /usr/local/bin/shlink
USER ${USER_ID}
EXPOSE 8080
ENTRYPOINT ["/bin/sh", "./docker-entrypoint.sh"]
