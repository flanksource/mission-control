FROM golang:1.20@sha256:bc5f0b5e43282627279fe5262ae275fecb3d2eae3b33977a7fd200c7a760d6f1 as builder
WORKDIR /app

ARG VERSION
COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
RUN go mod download
COPY ./ ./
RUN make build

FROM ubuntu:jammy@sha256:0bced47fffa3361afa981854fcabcd4577cd43cebbb808cea2b1f33a3dd7f508
WORKDIR /app

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && \
  apt-get install -y curl unzip ca-certificates jq tzdata wget gnupg2 bzip2 apt-transport-https lsb-release python3 python3-pip git --no-install-recommends && \
  rm -Rf /var/lib/apt/lists/*  && \
  rm -Rf /usr/share/doc && rm -Rf /usr/share/man  && \
  apt-get clean

RUN mkdir -p /etc/apt/keyrings && \
  curl -sLS https://packages.microsoft.com/keys/microsoft.asc | \
    gpg --dearmor | tee /etc/apt/keyrings/microsoft.gpg > /dev/null && \
  chmod go+r /etc/apt/keyrings/microsoft.gpg &&  \
  echo "deb [arch=`dpkg --print-architecture` signed-by=/etc/apt/keyrings/microsoft.gpg] https://packages.microsoft.com/repos/azure-cli/ $(lsb_release -cs) main" | tee /etc/apt/sources.list.d/azure-cli.list && \
  cat /etc/apt/sources.list.d/azure-cli.list && \
  apt-get update && \
  apt-get install -y azure-cli && \
  apt-get clean

RUN curl -sLS https://get.arkade.dev | sh

# Install CLIs with arkade as canary user
RUN arkade get --path /bin kubectl stern yq eksctl flux helm kustomize sops terraform

# Install AWS CLI
RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && \
  unzip -q awscliv2.zip && ./aws/install -i ${HOME}/aws -b ${HOME}/bin/ && \
  rm awscliv2.zip

# Install GCP CLI
ENV GCLOUD_VERSION=441.0.0
RUN curl -sL -O https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-${GCLOUD_VERSION}-linux-x86_64.tar.gz && \
  tar -xf google-cloud-cli-${GCLOUD_VERSION}-linux-x86_64.tar.gz && \
  ln -sf /app/google-cloud-sdk/bin/gcloud ${HOME}/bin/gcloud && \
  rm google-cloud-cli-${GCLOUD_VERSION}-linux-x86_64.tar.gz

COPY --from=builder /app/.bin/incident-commander /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

RUN /app/incident-commander go-offline
ENTRYPOINT ["/app/incident-commander"]
