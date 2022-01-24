FROM golang:1.17-alpine as builder

# Get target build variables
ARG TARGETOS
ARG TARGETARCH

# Setup
RUN mkdir -p /go/src/github.com/dbendit/traefik-forward-auth-plex-sso
WORKDIR /go/src/github.com/dbendit/traefik-forward-auth-plex-sso

# Add libraries
RUN apk add --no-cache git

# Copy & build
ADD . /go/src/github.com/dbendit/traefik-forward-auth-plex-sso/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GO111MODULE=on go build -a -installsuffix nocgo -o /traefik-forward-auth-plex-sso github.com/dbendit/traefik-forward-auth-plex-sso/cmd

# Copy into scratch container
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /traefik-forward-auth-plex-sso ./
ENTRYPOINT ["./traefik-forward-auth-plex-sso"]