#!/usr/bin/env bash

ip="$1"
port="$2"
conns="$3"
timeout="$4"

for i in $(seq 1 $conns);
do
  nc -v -w $timeout $ip $port < /dev/null &
done

wait
