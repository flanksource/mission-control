FROM golang:1.26-bookworm@sha256:f020456572fc292e9627b3fb435c6de5dfb8020fbcef1fd7b65dd092c0ac56bb AS builder
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
