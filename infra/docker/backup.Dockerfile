FROM postgres:17.4-alpine
RUN apk add --no-cache restic tzdata
COPY infra/backup/backup.sh /usr/local/bin/finance-backup
COPY infra/backup/restore-drill.sh /usr/local/bin/finance-restore-drill
ENTRYPOINT ["/usr/local/bin/finance-backup"]
