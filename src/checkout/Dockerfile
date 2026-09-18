# Static binary on scratch: ~9 MB, so an ECR push over hackathon wifi is seconds
# rather than minutes. There is no shell in the final image; debug through
# `kubectl logs` and the /version endpoint.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/checkout .

FROM scratch
# CA bundle only, so the OTLP exporter can verify the Elastic Cloud TLS chain.
# No shell added; still a single static binary otherwise.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/checkout /checkout
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/checkout"]
