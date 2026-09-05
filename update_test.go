package main

import (
	"bytes"
	"io"
	"testing"
)

func TestProgressReader(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 300*1024)

	type report struct {
		downloaded, total int64
	}
	var got []report
	pr := newProgressReader(bytes.NewReader(data), int64(len(data)), 64*1024,
		func(downloaded, total int64) {
			got = append(got, report{downloaded, total})
		})

	buf := make([]byte, 50*1024)
	for {
		if _, err := pr.Read(buf); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}

	if len(got) == 0 {
		t.Fatal("progressReader: no progress callbacks fired")
	}
	last := got[len(got)-1]
	if last.downloaded != int64(len(data)) || last.total != int64(len(data)) {
		t.Errorf("last report = %d/%d, want %d/%d", last.downloaded, last.total, len(data), len(data))
	}
	for i, r := range got {
		if r.downloaded < 64*1024 {
			t.Errorf("report %d below step threshold: %d bytes", i, r.downloaded)
		}
	}
}
