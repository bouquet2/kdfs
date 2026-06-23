package xfs

import (
	"errors"
	"reflect"
	"testing"
)

type fakeRunner struct {
	commands [][]string
	err      error
	output   []byte
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	f.commands = append(f.commands, append([]string{name}, args...))
	return f.output, f.err
}

func TestFormatRunsMkfsXFS(t *testing.T) {
	runner := &fakeRunner{}
	if err := Format("/dev/loop7", runner.Run); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"mkfs.xfs", "-f", "-s", "size=4096", "-m", "crc=0,finobt=0,rmapbt=0,reflink=0", "-i", "nrext64=0,maxpct=25", "-i", "bigtime=0,inobtcount=0", "/dev/loop7"}}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v", runner.commands)
	}
}

func TestEnsureMountedFormatsAndRetriesMount(t *testing.T) {
	runner := &fakeRunner{}
	attempts := 0
	formatted, err := EnsureMounted("/dev/nvme0n1", func() error {
		attempts++
		if attempts == 1 {
			return errors.New("needs format")
		}
		return nil
	}, runner.Run)
	if err != nil {
		t.Fatal(err)
	}
	if !formatted {
		t.Fatal("expected formatted to be true")
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
	want := [][]string{{"mkfs.xfs", "-f", "-s", "size=4096", "-m", "crc=0,finobt=0,rmapbt=0,reflink=0", "-i", "nrext64=0,maxpct=25", "-i", "bigtime=0,inobtcount=0", "/dev/nvme0n1"}}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v", runner.commands)
	}
}
