#!/bin/sh

: ${GOVENDOR:=govendor}

set -e -x

$GOVENDOR fetch -v \
          k8s.io/client-go/...@v5.0 \
          k8s.io/apimachinery/...@release-1.8 \
          k8s.io/api/...@release-1.8 \
          +missing +external

$GOVENDOR remove -v +unused

# This list should be empty
$GOVENDOR list +unused +missing +external
