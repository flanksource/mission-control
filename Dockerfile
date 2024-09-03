FROM golang:1.26-bookworm@sha256:f020456572fc292e9627b3fb435c6de5dfb8020fbcef1fd7b65dd092c0ac56bb AS builder
WORKDIR /app

ARG VERSION
COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
RUN go mod download
COPY ./ ./
RUN make build

FROM flanksource/base-image:main@sha256:fbf85eaa6ec4dfb67ab904401eae7e0a02d00068f5a924f560fa38e8e3276dbe
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
