FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY apps/worker/go.mod ./apps/worker/
COPY apps/reviewdomain/go.mod ./apps/reviewdomain/
WORKDIR /src
COPY apps/worker ./apps/worker
COPY apps/reviewdomain ./apps/reviewdomain
WORKDIR /src/apps/worker
RUN go mod download
RUN mkdir -p /out/attachments \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/finance-worker ./cmd/worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/finance-worker /finance-worker
COPY --chown=65532:65532 --from=build /out/attachments /var/lib/finance/attachments
USER nonroot:nonroot
ENTRYPOINT ["/finance-worker"]
