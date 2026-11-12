// Package quota provides immutable bounded quota accounting over uint64 units.
//
// The transition rules and invariants are proved for the Lean source. This Go
// facade delegates to its generated implementation; extraction and the facade
// are tested, trusted components, not a proved Go compiler. Callers choose which
// returned state becomes current and coordinate concurrent updates themselves.
package quota

import (
	"errors"

	generated "github.com/franchb/lean-go/generated/quota"
)

// ErrInvalidState reports use of a zero State. Construct states with New.
var ErrInvalidState = errors.New("invalid quota state")

// State is an immutable quota snapshot. Its zero value is invalid. Copying a
// State shares immutable storage; operations never change earlier snapshots.
type State struct {
	value generated.State
}

// New creates an unused quota, including capacities zero and math.MaxUint64.
func New(capacity uint64) (State, error) {
	value, err := generated.RunCreate(capacity, generated.DefaultLimits())
	if err != nil {
		return State{}, stateError(err)
	}
	return State{value: value}, nil
}

// Reserve adds amount to usage if it fits. A rejected request returns the
// unchanged state, false and nil. Zero units are accepted for every valid state.
// Errors return an invalid state and false.
func (s State) Reserve(amount uint64) (State, bool, error) {
	return transition(generated.RunReserve(s.value, amount, generated.DefaultLimits()))
}

// Release subtracts amount from usage if at least that much is in use. A
// rejected request returns the unchanged state, false and nil. It does not track
// ownership of individual reservations. Errors return an invalid state and false.
func (s State) Release(amount uint64) (State, bool, error) {
	return transition(generated.RunRelease(s.value, amount, generated.DefaultLimits()))
}

// Capacity returns the quota's fixed maximum usage.
func (s State) Capacity() (uint64, error) {
	value, ok := s.value.Capacity()
	if !ok {
		return 0, ErrInvalidState
	}
	return value, nil
}

// Used returns the currently reserved number of units.
func (s State) Used() (uint64, error) {
	value, ok := s.value.Used()
	if !ok {
		return 0, ErrInvalidState
	}
	return value, nil
}

// Remaining returns the number of units still available to reserve.
func (s State) Remaining() (uint64, error) {
	value, err := generated.RunRemaining(s.value, generated.DefaultLimits())
	return value, stateError(err)
}

func transition(out generated.Outcome, err error) (State, bool, error) {
	if err != nil {
		return State{}, false, stateError(err)
	}
	value, stateOK := out.State()
	accepted, acceptedOK := out.Accepted()
	if !stateOK || !acceptedOK {
		return State{}, false, ErrInvalidState
	}
	return State{value: value}, accepted, nil
}

func stateError(err error) error {
	if errors.Is(err, generated.ErrMalformed) {
		return ErrInvalidState
	}
	return err
}
