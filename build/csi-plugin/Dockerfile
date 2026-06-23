FROM golang:latest AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY internal/third_party/ ./internal/third_party/
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /out/csi-plugin ./cmd/csi-plugin

FROM alpine:latest
RUN apk add --no-cache nfs-utils xfsprogs
COPY --from=build /out/csi-plugin /bin/csi-plugin
ENTRYPOINT ["/bin/csi-plugin"]
