#!/bin/sh
go build -ldflags="-s -w" -o  LazyPLCNext.exe main.go
echo Done.