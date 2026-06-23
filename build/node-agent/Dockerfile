FROM golang:latest AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY internal/third_party/ ./internal/third_party/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/node-agent ./cmd/node-agent

FROM alpine:latest
RUN apk add --no-cache xfsprogs
COPY --from=build /out/node-agent /bin/node-agent
ENTRYPOINT ["/bin/node-agent"]
