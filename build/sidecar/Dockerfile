FROM golang:latest AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/sidecar ./cmd/sidecar

FROM scratch
COPY --from=build /out/sidecar /bin/sidecar
ENTRYPOINT ["/bin/sidecar"]
