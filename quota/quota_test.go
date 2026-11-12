package quota_test

import (
	"errors"
	"math/big"
	"math/rand"
	"testing"

	"github.com/franchb/lean-go/quota"
)

func requireState(t *testing.T, capacity uint64) quota.State {
	t.Helper()
	state, err := quota.New(capacity)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func assertSnapshot(t *testing.T, state quota.State, capacity, used uint64) {
	t.Helper()
	for _, check := range []struct {
		name string
		get  func() (uint64, error)
		want uint64
	}{
		{"capacity", state.Capacity, capacity},
		{"used", state.Used, used},
		{"remaining", state.Remaining, capacity - used},
	} {
		got, err := check.get()
		if err != nil || got != check.want {
			t.Fatalf("%s: got %d, %v; want %d", check.name, got, err, check.want)
		}
	}
}

// The oracle uses unbounded addition and comparisons; it does not repeat the
// source's cap-used guard or the generated uint64 overflow behavior.
func modelStep(capacity, used *big.Int, amount uint64, release bool) (*big.Int, bool) {
	units := new(big.Int).SetUint64(amount)
	next := new(big.Int).Set(used)
	if release {
		if units.Cmp(used) > 0 {
			return next, false
		}
		return next.Sub(next, units), true
	}
	next.Add(next, units)
	if next.Cmp(capacity) > 0 {
		return new(big.Int).Set(used), false
	}
	return next, true
}

func TestCapacityExtremesAndRejection(t *testing.T) {
	for _, capacity := range []uint64{0, 1, 1 << 32, 1<<53 + 1, 1 << 63, ^uint64(0)} {
		initial := requireState(t, capacity)
		full, accepted, err := initial.Reserve(capacity)
		if err != nil || !accepted {
			t.Fatalf("reserve capacity %d: %t, %v", capacity, accepted, err)
		}
		assertSnapshot(t, initial, capacity, 0)
		assertSnapshot(t, full, capacity, capacity)
		rejected, accepted, err := full.Reserve(1)
		if err != nil || accepted {
			t.Fatalf("over-capacity reserve %d: %t, %v", capacity, accepted, err)
		}
		assertSnapshot(t, rejected, capacity, capacity)
		empty, accepted, err := full.Release(capacity)
		if err != nil || !accepted {
			t.Fatalf("release capacity %d: %t, %v", capacity, accepted, err)
		}
		assertSnapshot(t, empty, capacity, 0)
		assertSnapshot(t, full, capacity, capacity)
		rejected, accepted, err = empty.Release(1)
		if err != nil || accepted {
			t.Fatalf("over-release %d: %t, %v", capacity, accepted, err)
		}
		assertSnapshot(t, rejected, capacity, 0)
		for _, operation := range []func(uint64) (quota.State, bool, error){full.Reserve, empty.Release} {
			_, accepted, err = operation(0)
			if err != nil || !accepted {
				t.Fatalf("zero units: %t, %v", accepted, err)
			}
		}
	}
}

func TestSequencesAgainstUnboundedModel(t *testing.T) {
	rng := rand.New(rand.NewSource(640071))
	capacities := []uint64{0, 1, 2, 31, 32, 1<<32 - 1, 1 << 32, 1<<53 + 1, 1 << 63, ^uint64(0)}
	for range 32 {
		capacities = append(capacities, rng.Uint64())
	}
	for index, capacity := range capacities {
		state := requireState(t, capacity)
		capModel, usedModel := new(big.Int).SetUint64(capacity), new(big.Int)
		for step := range 128 {
			before, beforeUsed := state, usedModel.Uint64()
			amounts := []uint64{0, 1, beforeUsed, capacity - beforeUsed, ^uint64(0), rng.Uint64()}
			amount, release := amounts[rng.Intn(len(amounts))], rng.Intn(2) == 0
			want, wantAccepted := modelStep(capModel, usedModel, amount, release)
			var next quota.State
			var accepted bool
			var err error
			if release {
				next, accepted, err = state.Release(amount)
			} else {
				next, accepted, err = state.Reserve(amount)
			}
			if err != nil || accepted != wantAccepted {
				t.Fatalf("capacity[%d]=%d step %d release=%t amount=%d: accepted=%t err=%v want=%t", index, capacity, step, release, amount, accepted, err, wantAccepted)
			}
			assertSnapshot(t, next, capacity, want.Uint64())
			assertSnapshot(t, before, capacity, beforeUsed)
			state, usedModel = next, want
		}
	}
}

func TestZeroStateIsInvalid(t *testing.T) {
	var state quota.State
	for _, get := range []func() (uint64, error){state.Capacity, state.Used, state.Remaining} {
		value, err := get()
		if value != 0 || !errors.Is(err, quota.ErrInvalidState) {
			t.Fatalf("zero state read: %d, %v", value, err)
		}
	}
	for _, operation := range []func(uint64) (quota.State, bool, error){state.Reserve, state.Release} {
		next, accepted, err := operation(0)
		if accepted || !errors.Is(err, quota.ErrInvalidState) {
			t.Fatalf("zero state transition: %t, %v", accepted, err)
		}
		if _, err := next.Capacity(); !errors.Is(err, quota.ErrInvalidState) {
			t.Fatal("error returned a valid state")
		}
	}
	assertSnapshot(t, requireState(t, 0), 0, 0)
}
