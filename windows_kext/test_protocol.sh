#!/bin/sh
echo Running tests
echo ========================
cd protocol
cargo test

cd ../kextinterface
go test -v .
