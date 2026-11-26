import LeanGo.Validate
import LeanGo.Validate.Meta
import LeanGo.Validate.Meta.Axioms
import Examples.Clamp
import Examples.Flags
import Examples.Words
import Examples.Crc16
import Examples.Adler32
import Examples.Quota
import Examples.Trees
import Examples.Counters
import Examples.Records
import Examples.Reduced

/-! Build gate: every declaration of the library and of the examples uses only
`propext`, `Classical.choice` and `Quot.sound`. Fails the build otherwise. -/

#assert_standard_axioms LeanGo.Validate Examples

/-! The headline statements, printed for the record. -/
#print axioms LeanGo.Validate.run_sound
#print axioms LeanGo.Validate.run_le
#print axioms Examples.Trees.mirror_sound
#print axioms Examples.Adler32.update_sound
