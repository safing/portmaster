#!/bin/sh

echo Generate test files
echo ========================
cd protocol
cargo test info::generate_test_info_file

cd ../kextinterface
go test -v -run TestGenerateCommandFile

cd ..
echo ========================
echo Running tests
echo ========================
cd protocol
cargo test command::test_go_command_file

cd ../kextinterface
go test -v -run TestRustInfoFile

echo ========================
echo Cleanup
rm go_command_test.bin
rm ../protocol/rust_info_test.bin
