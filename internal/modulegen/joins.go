package modulegen

import (
	"bytes"
	"fmt"

	"github.com/franchb/lean-go/internal/moduleir"
)

func (g *generator) emitJump(w *bytes.Buffer, f *function, c *moduleir.Code) {
	j := f.joins[c.Target]
	if len(j.Params) > 0 {
		for i, param := range j.Params {
			if i > 0 {
				w.WriteByte(',')
			}
			fmt.Fprintf(w, "f.slots[%d]", f.slots[param.ID])
		}
		w.WriteByte('=')
		for i, id := range c.Args {
			if i > 0 {
				w.WriteByte(',')
			}
			fmt.Fprintf(w, "f.slots[%d]", f.slots[id])
		}
		w.WriteByte('\n')
	}
	fmt.Fprintf(w, "f.pc=%d\n", f.states[j.Body])
}
