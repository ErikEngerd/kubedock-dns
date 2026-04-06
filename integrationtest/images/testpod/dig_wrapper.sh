#!/bin/sh

# Wrapper for dig that echoes only IP addresses
if [ -z "$1" ]; then
  echo "Usage: dig_wrapper.sh <hostname>"
  exit 1
fi

# Run dig and filter output
# +short returns only IPs; filter out empty lines
ip=$(dig +short "$1" | grep -E '^[0-9.]+$')
if [ -z "$ip" ]; then
  exit 1
fi

echo "$ip"
