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
RUN apk add --no-cache git git-lfs ca-certificates
COPY --from=builder /githubby /githubby
ENV GITHUBBY_NO_UPDATE=1
ENTRYPOINT ["/githubby"]
