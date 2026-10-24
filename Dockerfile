FROM golang:1.27 AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.io,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/app

FROM gcr.io/distroless/static-debian12
COPY --from=build /app /app
CMD ["/app"]
