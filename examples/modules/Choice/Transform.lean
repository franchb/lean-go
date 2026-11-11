import Choice.Model

namespace Choice

def rotate : Signal → Signal
  | .stop => .wait
  | .wait => .go
  | .go => .stop

end Choice
