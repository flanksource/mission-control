FROM node:16 as node
WORKDIR /app
COPY "./ui/package.json" .
COPY "./ui/package-lock.json" .
RUN npm i
ADD ui/ .
RUN  ls && npm run build

FROM golang:1.17 as builder
WORKDIR /app
COPY go.mod go.mod
COPY go.sum go.sum
ARG VERSION
RUN go mod download
COPY --from=node /app/build /app/ui/build
COPY ./ ./
RUN make build

FROM ubuntu:bionic
WORKDIR /app

# install CA certificates
RUN apt-get update && \
  apt-get install -y ca-certificates && \
  rm -Rf /var/lib/apt/lists/*  && \
  rm -Rf /usr/share/doc && rm -Rf /usr/share/man  && \
  apt-get clean

COPY --from=builder /app/.bin/incident-commander /app

RUN /app/incident-commander go-offline
RUN curl -sSfL https://raw.githubusercontent.com/cosmtrek/air/master/install.sh | sh -s
RUN air -v

ENTRYPOINT ["/app/incident-commander"]
