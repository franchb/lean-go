package differential

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/franchb/lean-go/internal/modulegen"
	"github.com/franchb/lean-go/internal/moduleir"
	"github.com/franchb/lean-go/internal/pipeline"
)

// Since lean-go-modulegen/v20 a statically bounded metered root runs the
// native emitter's code (the fast path) when the limits cover its static
// bound, in the typed entry points and in Run/RunRoot alike. Every generated
// program is bounded, so the raw and typed channels run that code. The
// state-machine channels compare a second metered package generated in
// process from the same IR by modulegen.GenerateStateMachine, which leaves
// the fast path out: every root runs on the explicit-stack state machine.
// There is deliberately no command-line flag for this variant.

// EmitStateMachine writes the state-machine package of the IR file irPath
// into out.
func EmitStateMachine(irPath, out string) error {
	data, err := os.ReadFile(irPath)
	if err != nil {
		return err
	}
	program, err := moduleir.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode IR: %w", err)
	}
	files, err := modulegen.GenerateStateMachine(program)
	if err != nil {
		return err
	}
	return pipeline.PublishPackage(out, files, false)
}

// emitStateMachine runs b.StateMachine as the logged in-process stage
// "state-machine-emit".
func (b *Backend) emitStateMachine(irPath, out string) error {
	record := commandRecord{Stage: "state-machine-emit", Command: []string{"(in process)", "modulegen.GenerateStateMachine", irPath, out},
		Cwd: b.Repo, TimeoutSeconds: b.Timeout.Seconds(), Environment: map[string]string{}}
	start := time.Now()
	err := b.StateMachine(irPath, out)
	record.ElapsedSeconds = time.Since(start).Seconds()
	code := 0
	if err != nil {
		record.Error, code = err.Error(), 1
	}
	record.Returncode = &code
	if logErr := b.log(record); logErr != nil {
		return logErr
	}
	if err != nil {
		return backendError("state-machine-emit", "%v", err)
	}
	return nil
}
