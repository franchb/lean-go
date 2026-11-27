namespace Trees

inductive Color where
  | red
  | green
  | blue

inductive Tree where
  | leaf : Color → Tree
  | branch : Tree → Tree → Tree

def recolor : Color → Color
  | .red => .blue
  | .green => .green
  | .blue => .red

def mirror : Tree → Tree
  | .leaf c => .leaf (recolor c)
  | .branch left right => .branch (mirror right) (mirror left)

def scalar (n : UInt32) : UInt32 := n

end Trees
