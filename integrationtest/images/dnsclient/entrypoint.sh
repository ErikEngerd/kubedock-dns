#!/bin/bash


timeout="$1"
ready="$2"
shift
shift
if [[ $# -eq 0 ]]
then
  set tail -f
fi

function countdown
{
  count="$1"
  while [[ $count -ne 0 ]]
  do
    echo "stop in $count seconds"
    sleep 1
    count="$(( $count -1 ))"
  done
}

# Trap SIGTERM and SIGINT
trap "echo caught signal; countdown $timeout; exit" EXIT

if [[ "$ready" = "true" ]]
then
  touch /ready
fi

echo "Started"
# Execute passed command
"$@"
