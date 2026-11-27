namespace Clock

/-- A duration split into whole hours, minutes and seconds. -/
structure Hms where
  hours : UInt32
  minutes : UInt32
  seconds : UInt32

/-- Split a number of seconds with unsigned division and remainder. -/
def split (total : UInt32) : Hms :=
  ⟨total / 3600, total % 3600 / 60, total % 60⟩

/-- Recombine hours, minutes and seconds into seconds, wrapping like `UInt32`. -/
def join (time : Hms) : UInt32 :=
  time.hours * 3600 + time.minutes * 60 + time.seconds

end Clock
