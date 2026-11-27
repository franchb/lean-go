import Clock

namespace ClockContract

-- Independent requirements: these definitions do not import or reuse the proofs.
def recombines : Prop := ∀ total : UInt32, Clock.join (Clock.split total) = total

def minutesBelow60 : Prop := ∀ total : UInt32, (Clock.split total).minutes < 60

def secondsBelow60 : Prop := ∀ total : UInt32, (Clock.split total).seconds < 60

end ClockContract
