FROM golang:1.26.1-bookworm@sha256:ab3d6955bbc813a0f3fdf220c1d817dd89c0b3f283777db8ece4a32fe7858edd AS builder
WORKDIR /app

ARG VERSION
COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
COPY plugin/api/go.mod /app/plugin/api/go.mod
COPY plugin/api/go.sum /app/plugin/api/go.sum
COPY plugin/sdk/go.mod /app/plugin/sdk/go.mod
COPY plugin/sdk/go.sum /app/plugin/sdk/go.sum
RUN go mod download
COPY ./ ./
RUN make build

FROM flanksource/base-image:0.6.0@sha256:6cae0a4bbba7e7e16674a55751c8161c11d5ebdd23f596f93e669f835ee1e034
WORKDIR /app

ARG TARGETARCH
ARG NODE_VERSION=24.11.0
# Pin to a version where both the release binary and the matching
# @flanksource/facet npm package (installed into .facet/ at render time) exist.
# v0.1.40 ships a binary but no npm package, so the render-time pnpm install fails.
ARG FACET_VERSION=v0.1.39

# Facet renders report PDFs via a headless Chrome launched by Puppeteer.
# Install the shared libraries Chrome needs plus fonts for correct rendering.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    fonts-liberation \
    fonts-noto-color-emoji \
    libasound2t64 \
    libatk-bridge2.0-0t64 \
    libatk1.0-0t64 \
    libcairo2 \
    libcups2t64 \
    libdbus-1-3 \
    libexpat1 \
    libfontconfig1 \
    libgbm1 \
    libglib2.0-0t64 \
    libgtk-3-0t64 \
    libnspr4 \
    libnss3 \
    libpango-1.0-0 \
    libpangocairo-1.0-0 \
    libx11-6 \
    libx11-xcb1 \
    libxcb1 \
    libxcomposite1 \
    libxcursor1 \
    libxdamage1 \
    libxext6 \
    libxfixes3 \
    libxi6 \
    libxrandr2 \
    libxrender1 \
    libxtst6 \
    xdg-utils \
    && rm -rf /var/lib/apt/lists/* && apt-get clean

# Node (with npm/npx) is needed to install Puppeteer's Chrome. deps is already
# present in the base image and is the same installer the Makefile uses. The
# GITHUB_TOKEN secret lifts the GitHub API rate limit deps hits resolving
# nodejs/node releases.
# facet installs report template deps with pnpm at render time, so it must be
# on PATH. Symlink it into /usr/bin since npm's global bin dir is not.
ARG PNPM_VERSION=9.15.9
RUN --mount=type=secret,id=GITHUB_TOKEN,env=GITHUB_TOKEN \
    deps install node@${NODE_VERSION} --bin-dir /usr/bin --app-dir /usr/lib/node --tmp-dir /tmp && \
    npm install -g pnpm@${PNPM_VERSION} && \
    ln -sf "$(npm prefix -g)/bin/pnpm" /usr/bin/pnpm && \
    pnpm --version

# Install the facet standalone binary (bun-compiled, self-contained). The npm
# package has no bin entry and unbundled deps, so the release binary is the
# supported way to run `facet`. A GITHUB_TOKEN secret is used (when present) to
# avoid GitHub API/download rate limits.
RUN --mount=type=secret,id=GITHUB_TOKEN,env=GITHUB_TOKEN \
    FACET_ARCH=$([ "${TARGETARCH}" = "amd64" ] && echo x64 || echo "${TARGETARCH}") && \
    CURL_CFG=$(mktemp) && \
    [ -n "${GITHUB_TOKEN}" ] && printf 'header = "Authorization: Bearer %s"\n' "${GITHUB_TOKEN}" > "${CURL_CFG}"; \
    if [ "${FACET_VERSION}" = "latest" ]; then \
      FACET_URL=$(curl -fsSL -K "${CURL_CFG}" https://api.github.com/repos/flanksource/facet/releases/latest | jq -r ".assets[] | select(.name == \"facet-linux-${FACET_ARCH}\") | .browser_download_url"); \
    else \
      FACET_URL="https://github.com/flanksource/facet/releases/download/${FACET_VERSION}/facet-linux-${FACET_ARCH}"; \
    fi && \
    curl -fsSL -K "${CURL_CFG}" "${FACET_URL}" -o /usr/local/bin/facet && \
    rm -f "${CURL_CFG}" && \
    chmod +x /usr/local/bin/facet && \
    facet --version

# Install Chrome for Puppeteer and symlink it to a stable path (the install dir
# is version-specific, so ENV can't reference it with a glob).
ENV PUPPETEER_CACHE_DIR=/opt/puppeteer
RUN npx --yes puppeteer browsers install chrome --path /opt/puppeteer && \
    ln -sf "$(ls /opt/puppeteer/chrome/linux-*/chrome-linux64/chrome)" /usr/local/bin/chrome
ENV PUPPETEER_EXECUTABLE_PATH=/usr/local/bin/chrome

# Verify facet can render a PDF inside the container.
COPY fixtures/facet/SimpleReport.tsx fixtures/facet/simple-data.json /tmp/facet/
RUN cd /tmp/facet && \
    facet pdf SimpleReport.tsx --data simple-data.json --output /tmp/facet/out.pdf && \
    test -s /tmp/facet/out.pdf && \
    [ "$(stat -c%s /tmp/facet/out.pdf)" -ge 1024 ] && \
    echo "facet PDF render OK: $(stat -c%s /tmp/facet/out.pdf) bytes" && \
    rm -rf /tmp/facet

COPY --from=builder /app/.bin/incident-commander /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

RUN /app/incident-commander go-offline
ENTRYPOINT ["/app/incident-commander"]
