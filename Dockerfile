
FROM alpine:3.20.2 as builder

RUN apk update && apk add go
RUN mkdir -p /opt/dns/bin
WORKDIR /opt/dns

ENV CGO_ENABLED=0
ENV GOTOOLCHAIN=auto

COPY go.mod go.sum /opt/dns/
RUN go mod download
COPY cmd    /opt/dns/cmd/
COPY internal /opt/dns/internal/

RUN go build -o bin ./cmd/...
RUN find . -type f

FROM scratch

COPY --from=builder /opt/dns/bin/dns-server /opt/dns/bin/

ENTRYPOINT ["/opt/dns/bin/dns-server" ]
