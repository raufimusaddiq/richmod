FROM golang:1.27.0-alpine AS build
RUN GOBIN=/out go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/goose /goose
COPY db/migrations /migrations
USER nonroot:nonroot
ENTRYPOINT ["/goose"]
