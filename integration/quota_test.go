package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	q "github.com/franchb/lean-go/generated/quota"
)

type quotaSample struct {
	capacity, used, amount uint64
	release                bool
}
type quotaResult [6]uint64

func quotaSamples() []quotaSample {
	boundary := []uint64{0, 1, 2, 31, 1<<32 - 1, 1<<53 + 1, 1<<64 - 1}
	var samples []quotaSample
	for _, capacity := range boundary {
		for _, used := range []uint64{0, capacity, 1<<64 - 1} {
			for _, amount := range boundary {
				for _, release := range []bool{false, true} {
					samples = append(samples, quotaSample{capacity, used, amount, release})
				}
			}
		}
	}
	rng := rand.New(rand.NewSource(640712))
	for range 128 {
		samples = append(samples, quotaSample{rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Intn(2) == 1})
	}
	return samples
}

func expectedQuota(sample quotaSample) quotaResult {
	capacity, used, amount := new(big.Int).SetUint64(sample.capacity), new(big.Int).SetUint64(sample.used), new(big.Int).SetUint64(sample.amount)
	result := quotaResult{0, sample.capacity, sample.used, 0, sample.capacity, 0}
	if used.Cmp(capacity) > 0 {
		return result
	}
	available := new(big.Int).Sub(capacity, used)
	if sample.release {
		if amount.Cmp(used) <= 0 {
			used.Sub(used, amount)
			result[0] = 1
		}
	} else if amount.Cmp(available) <= 0 {
		used.Add(used, amount)
		result[0] = 1
	}
	result[2] = used.Uint64()
	result[3] = new(big.Int).Sub(capacity, used).Uint64()
	return result
}

func quotaField[T any](t *testing.T) func(T, bool) T {
	t.Helper()
	return func(value T, ok bool) T {
		t.Helper()
		if !ok {
			t.Fatal("invalid generated field")
		}
		return value
	}
}

func typedQuota(t *testing.T, sample quotaSample) quotaResult {
	t.Helper()
	l := q.DefaultLimits()
	state := requireTyped[q.State](t)(q.NewState(sample.capacity, sample.used, l))
	run := q.RunReserve
	if sample.release {
		run = q.RunRelease
	}
	outcome := requireTyped[q.Outcome](t)(run(state, sample.amount, l))
	next := quotaField[q.State](t)(outcome.State())
	initial := requireTyped[q.State](t)(q.RunCreate(sample.capacity, l))
	accepted := uint64(0)
	if quotaField[bool](t)(outcome.Accepted()) {
		accepted = 1
	}
	return quotaResult{accepted, quotaField[uint64](t)(next.Capacity()), quotaField[uint64](t)(next.Used()),
		requireTyped[uint64](t)(q.RunRemaining(next, l)), quotaField[uint64](t)(initial.Capacity()), quotaField[uint64](t)(initial.Used())}
}

func rawQuota(t *testing.T, sample quotaSample) quotaResult {
	t.Helper()
	l := q.DefaultLimits()
	number := func(value uint64) *q.Input { return &q.Input{Type: "UInt64", UInt64: value} }
	state := requireTyped[q.Value](t)(q.Import(&q.Input{Type: "Quota.State", Constructor: "Quota.State.mk", Fields: []*q.Input{number(sample.capacity), number(sample.used)}}, l))
	amount := requireTyped[q.Value](t)(q.Import(number(sample.amount), l))
	run := func(name string, args ...q.Value) q.Value {
		return requireTyped[q.Value](t)(q.RunRoot("Quota."+name, args, l))
	}
	operation := "reserve"
	if sample.release {
		operation = "release"
	}
	outcome := run(operation, state, amount)
	fields := outcome.Fields()
	if outcome.Type() != "Quota.Outcome" || len(fields) != 2 {
		t.Fatal("invalid raw outcome")
	}
	next := fields[0]
	accepted := uint64(0)
	switch fields[1].Constructor() {
	case "Bool.true":
		accepted = 1
	case "Bool.false":
	default:
		t.Fatal("invalid accepted flag")
	}
	readState := func(value q.Value) [2]uint64 {
		fields := value.Fields()
		if value.Type() != "Quota.State" || len(fields) != 2 {
			t.Fatal("invalid raw state")
		}
		return [2]uint64{quotaField[uint64](t)(fields[0].UInt64()), quotaField[uint64](t)(fields[1].UInt64())}
	}
	current := readState(next)
	capacity := requireTyped[q.Value](t)(q.Import(number(sample.capacity), l))
	initial := readState(run("create", capacity))
	return quotaResult{accepted, current[0], current[1], quotaField[uint64](t)(run("remaining", next).UInt64()), initial[0], initial[1]}
}

func TestQuotaRawTypedSemantics(t *testing.T) {
	for i, sample := range quotaSamples() {
		want := expectedQuota(sample)
		if raw, typed := rawQuota(t, sample), typedQuota(t, sample); raw != want || typed != want {
			t.Fatalf("sample %d: raw%v typed%v want%v", i, raw, typed, want)
		}
	}
}

func TestQuotaNativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native quota comparison")
	}
	dir := t.TempDir()
	var reference strings.Builder
	reference.WriteString("import Quota\nstructure Sample where\n  capacity : UInt64\n  used : UInt64\n  amount : UInt64\n  release : Bool\ndef samples : List Sample := [\n")
	for i, sample := range quotaSamples() {
		if i > 0 {
			reference.WriteString(",\n")
		}
		fmt.Fprintf(&reference, "{capacity:=%d, used:=%d, amount:=%d, release:=%t}", sample.capacity, sample.used, sample.amount, sample.release)
	}
	reference.WriteString(`]
def encode (sample : Sample) : String := Id.run do
  let state : Quota.State := ⟨sample.capacity, sample.used⟩
  let outcome := if sample.release then Quota.release state sample.amount else Quota.reserve state sample.amount
  let initial := Quota.create sample.capacity
  let values := [toString (if outcome.accepted then (1 : Nat) else 0),
    toString outcome.state.capacity.toNat, toString outcome.state.used.toNat,
    toString (Quota.remaining outcome.state).toNat, toString initial.capacity.toNat, toString initial.used.toNat]
  return "[" ++ String.intercalate "," values ++ "]"
def main : IO Unit := IO.println ("[" ++ String.intercalate "," (samples.map encode) ++ "]")
`)
	files := map[string]string{"Reference.lean": reference.String(), "lakefile.toml": "name = \"quota_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Quota\"\n[[lean_exe]]\nname = \"reference\"\nroot = \"Reference\"\n"}
	for target, path := range map[string]string{"Quota.lean": "../libraries/quota/source/Quota.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[target] = string(data)
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command("lake", "build", "reference")
	cmd.Dir, cmd.Env = dir, commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native build: %v\n%s", err, out)
	}
	cmd = exec.Command(filepath.Join(dir, ".lake/build/bin/reference"))
	cmd.Dir, cmd.Env = dir, commandEnv(nil)
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var actual []quotaResult
	if err = json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	samples := quotaSamples()
	if len(actual) != len(samples) {
		t.Fatal("native result count mismatch")
	}
	for i, sample := range samples {
		if want := expectedQuota(sample); actual[i] != want || typedQuota(t, sample) != want || rawQuota(t, sample) != want {
			t.Fatalf("native sample %d: got%v want%v", i, actual[i], want)
		}
	}
}

func TestQuotaRegeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_REGENERATE") != "1" {
		t.Skip("set LEAN_GO_REGENERATE=1 for quota regeneration")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out, ir := filepath.Join(dir, "quota"), filepath.Join(dir, "quota.ir.json")
	args := []string{"run", "--locked", "python", "scripts/generate.py", "--source-dir", "libraries/quota/source", "--module", "Quota",
		"--root", "Quota.reserve", "--root", "Quota.remaining", "--root", "Quota.release", "--root", "Quota.create", "--out", out, "--ir-output", ir}
	cmd := exec.Command("uv", args...)
	cmd.Dir, cmd.Env = root, commandEnv(nil)
	if log, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("regeneration: %v\n%s", err, log)
	}
	if !reflect.DeepEqual(directoryFiles(t, out), directoryFiles(t, "../generated/quota")) {
		t.Fatal("quota regeneration changed package")
	}
	actual, err := os.ReadFile(ir)
	if err != nil {
		t.Fatal(err)
	}
	tracked, err := os.ReadFile("../testdata/quota.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, tracked) {
		t.Fatal("quota regeneration changed IR")
	}
}
