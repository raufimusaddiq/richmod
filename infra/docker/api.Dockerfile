FROM node:26.12.1-alpine AS web
WORKDIR /web
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY apps/web ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY apps/api/go.mod ./apps/api/
COPY apps/reviewdomain/go.mod ./apps/reviewdomain/
WORKDIR /src
COPY apps/api ./apps/api
COPY apps/reviewdomain ./apps/reviewdomain
RUN rm -rf ./apps/api/internal/webui/assets
COPY --from=web /web/out ./apps/api/internal/webui/assets
WORKDIR /src/apps/api
RUN go mod download
RUN mkdir -p /out/attachments \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/finance-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/bootstrap ./cmd/bootstrap

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/finance-api /finance-api
COPY --from=build /out/bootstrap /bootstrap
COPY --chown=65532:65532 --from=build /out/attachments /var/lib/finance/attachments
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/finance-api"]
