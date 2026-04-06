FROM alpine:3.23.3

# Install dig
RUN apk add --no-cache bash bind-tools

WORKDIR /app

# Copy scripts
COPY *.sh /app/
RUN chmod +x /app/*.sh

ENTRYPOINT ["/app/entrypoint.sh"]

