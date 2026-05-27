FROM golang:latest AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/nfs-sidecar ./cmd/nfs-sidecar

FROM scratch
COPY --from=builder /out/nfs-sidecar /nfs-sidecar
ENTRYPOINT ["/nfs-sidecar"]
