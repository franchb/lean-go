package quota_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStandaloneGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	for _, relative := range []string{"quota", "generated/quota"} {
		copyConsumerPackage(t, filepath.Join("..", relative), filepath.Join(dir, relative))
	}
	files := map[string]string{
		"go.mod": "module github.com/franchb/lean-go\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";"github.com/franchb/lean-go/quota")
func main(){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc","clang","cl"}{if _,err:=exec.LookPath(name);err==nil{panic("producer available")}}
s,err:=quota.New(18446744073709551615);if err!=nil{panic(err)}
full,ok,err:=s.Reserve(18446744073709551615);if err!=nil||!ok{panic("full reserve")}
denied,ok,err:=full.Reserve(1);if err!=nil||ok{panic("overflow accepted")}
used,err:=denied.Used();if err!=nil||used!=18446744073709551615{panic("rejection changed state")}
empty,ok,err:=full.Release(18446744073709551615);if err!=nil||!ok{panic("release")}
remaining,err:=empty.Remaining();if err!=nil||remaining!=18446744073709551615{panic("remaining")}
initialUsed,err:=s.Used();if err!=nil||initialUsed!=0{panic("initial state changed")}
fmt.Println("quota Go-only consumer: OK")
}`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	goName := "go"
	if runtime.GOOS == "windows" {
		goName += ".exe"
	}
	goPath := filepath.Join(runtime.GOROOT(), "bin", goName)
	command := exec.Command(goPath, "run", "-buildvcs=false", ".")
	command.Dir = dir
	// exec.Cmd keeps the last value for duplicate environment keys.
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off",
		"GOPROXY=off", "GOSUMDB=off", "GOFLAGS=", "PATH="+filepath.Dir(goPath))
	output, err := command.CombinedOutput()
	if err != nil || string(output) != "quota Go-only consumer: OK\n" {
		t.Fatalf("consumer: %v\n%s", err, output)
	}
}

func copyConsumerPackage(t *testing.T, source, destination string) {
	t.Helper()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, entry.Name()), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
