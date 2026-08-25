FROM golang:1.24.1-alpine AS build
WORKDIR /src
COPY apps/api/go.mod ./apps/api/
WORKDIR /src/apps/api
RUN go mod download
WORKDIR /src
COPY apps/api ./apps/api
WORKDIR /src/apps/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/finance-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/bootstrap ./cmd/bootstrap

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/finance-api /finance-api
COPY --from=build /out/bootstrap /bootstrap
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/finance-api"]
