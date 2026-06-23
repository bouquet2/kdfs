FROM golang:latest AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY internal/third_party/ ./internal/third_party/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/controller-manager ./cmd/controller-manager

FROM scratch
COPY --from=build /out/controller-manager /bin/controller-manager
ENTRYPOINT ["/bin/controller-manager"]
