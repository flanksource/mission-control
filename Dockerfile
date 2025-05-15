FROM golang:1.24-bookworm@sha256:89a04cc2e2fbafef82d4a45523d4d4ae4ecaf11a197689036df35fef3bde444a AS builder
WORKDIR /app

ARG VERSION
COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
RUN go mod download
COPY ./ ./
RUN make build

FROM flanksource/base-image:0.5.15@sha256:8d3fe5816e10e0eb0e74ef30dbbc66d54402dcbdab80b72c7461811a05825dbc
WORKDIR /app

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && \
  apt-get install -y python3 python3-pip --no-install-recommends && \
  rm -Rf /var/lib/apt/lists/*  && \
  rm -Rf /usr/share/doc && rm -Rf /usr/share/man  && \
  apt-get clean

RUN arkade get --path /usr/bin eksctl flux helm kustomize terraform && \
  chmod +x /usr/bin/eksctl /usr/bin/flux /usr/bin/helm /usr/bin/kustomize /usr/bin/terraform

COPY --from=builder /app/.bin/incident-commander /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

RUN /app/incident-commander go-offline
ENTRYPOINT ["/app/incident-commander"]
