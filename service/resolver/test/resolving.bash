#!/bin/bash

DOMAINS="twitter.com news.ycombinator.com"

while true; do
  for domain in $DOMAINS; do
    # query domain
    Q=$(dig $domain | tr '\n' '§')
    # check result
    if [[ $(echo $Q | grep NOERROR | wc -l) -gt 0 ]]; then
      echo "$(date "+%y%m%d %H:%M:%S") [OK] $domain ($(echo $Q | grep -aoE 'valid for [a-z0-9]+'))"
    else
      echo ""
      echo "$(date "+%y%m%d %H:%M:%S") [FAILED] $domain"
      echo $Q | tr '§' '\n'
      echo "#####"
      echo ""
    fi
    # wait
    sleep 5
  done
done
