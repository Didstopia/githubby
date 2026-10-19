FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev COMMIT=unknown BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -ldflags="-s -w \
    -X main.Version=${VERSION} \
    -X main.Commit=${COMMIT} \
    -X main.BuildDate=${BUILD_DATE}" \
    -o /githubby .

FROM alpine:3.22
# The backup may be owned by another uid than the container runs as, so trust every repo.
# LFS filters go in the system config, so every uid has them.
# tzdata lets TZ (e.g. Europe/Helsinki) set the zone --schedule runs in; UTC without it
# tini reaps the background processes git leaves behind (auto gc); without it they fill the pid limit
RUN apk add --no-cache git git-lfs ca-certificates tzdata tini \
    && git config --system --add safe.directory '*' \
    && git lfs install --system --skip-repo
COPY --from=builder /githubby /githubby
ENV GITHUBBY_NO_UPDATE=1
# A uid with no home (e.g. Unraid's 99:100) gets HOME=/, which it can't write; root keeps /root
ENTRYPOINT ["/sbin/tini", "--", "/bin/sh", "-c", "[ -w \"$HOME\" ] || export HOME=/tmp; exec /githubby \"$@\"", "githubby"]
