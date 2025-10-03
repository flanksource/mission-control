FROM golang:1.25-bookworm@sha256:c423747fbd96fd8f0b1102d947f51f9b266060217478e5f9bf86f145969562ee AS builder
WORKDIR /app

ARG VERSION
COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
RUN go mod download
COPY ./ ./
RUN make build

FROM flanksource/base-image:0.5.20@sha256:5a29745a30b1f316da822b78863fd21ae7a8058c1f475ff912a8e787a9e40562
WORKDIR /app

COPY --from=builder /app/.bin/incident-commander /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

RUN /app/incident-commander go-offline
ENTRYPOINT ["/app/incident-commander"]
