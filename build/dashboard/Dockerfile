FROM golang:latest AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/dashboard ./cmd/dashboard

FROM scratch
COPY --from=build /out/dashboard /bin/dashboard
ENTRYPOINT ["/bin/dashboard"]
