#!/bin/sh
go build -ldflags="-s -w" -o LazyPLCNext.exe .
echo Done.