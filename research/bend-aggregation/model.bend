import Base

# The model consumes histogram indices, not source metrics. Production indices
# are bit lengths in 0..64. This nonempty representation saturates excess
# indices to its last cell; that fallback is outside the admitted input domain.
type Histogram is Data:
  Last{count: Nat}
  Cell{count: Nat, rest: Histogram}

def zero(extra_cells: Nat) -> Histogram:
  match extra_cells:
    case 0n:
      Last{0n}
    case 1n+p:
      Cell{0n, zero(p)}

def total(h: Histogram) -> Nat:
  match h:
    case Last{count}:
      count
    case Cell{count, rest}:
      Nat.add(count, total(rest))

def bump(index: Nat, h: Histogram) -> Histogram:
  match index h:
    case 0n Last{count}:
      Last{1n+count}
    case 0n Cell{count, rest}:
      Cell{1n+count, rest}
    case 1n+p Last{count}:
      Last{1n+count}
    case 1n+p Cell{count, rest}:
      Cell{count, bump(p, rest)}

def aggregate(xs: +List<Nat>, h: Histogram) -> Histogram:
  match xs:
    case Nil{}:
      h
    case Con{x, tail}:
      aggregate(tail, bump(x, h))

def length(xs: +List<Nat>) -> Nat:
  match xs:
    case Nil{}:
      0n
    case Con{x, tail}:
      1n+length(tail)

def append(xs: +List<Nat>, ys: +List<Nat>) -> +List<Nat>:
  match xs:
    case Nil{}:
      ys
    case Con{x, tail}:
      Con{x, append(tail, ys)}

def increment(matches: Bool, count: Nat) -> Nat:
  match matches:
    case True{}:
      1n+count
    case False{}:
      count

def count_bin(xs: +List<Nat>, +index: Nat) -> Nat:
  match xs:
    case Nil{}:
      0n
    case Con{x, tail}:
      increment(Nat.is_eq(x, index), count_bin(tail, index))

def lookup(index: Nat, h: Histogram) -> Nat:
  match index h:
    case 0n Last{count}:
      count
    case 0n Cell{count, rest}:
      count
    case 1n+p Last{count}:
      count
    case 1n+p Cell{count, rest}:
      lookup(p, rest)

# Runtime mapping is deliberately restricted to U32 metric values. The
# production classifier accepts U64; its upper half is tested outside Bend.
def bit_length_go(fuel: Nat, +value: U32) -> Nat:
  match fuel:
    case 0n:
      0n
    case 1n+p:
      increment(U32.is_ne(value, 0), bit_length_go(p, U32.shr(value)))

def bit_length(value: U32) -> Nat:
  bit_length_go(32n, value)

def bins(values: +List<U32>) -> +List<Nat>:
  match values:
    case Nil{}:
      Nil{}
    case Con{value, rest}:
      Con{bit_length(value), bins(rest)}

# A finite certificate ties the dense representation to direct bin counting.
# It exercises every ordered pair of admitted bin indices.
def pair_ok(+left: Nat, +right: Nat) -> Bool:
  +xs = {[left, right] : +List<Nat>}
  +h = aggregate(xs, zero(64n))
  Nat.is_eq(total(h), 2n) && Nat.is_eq(lookup(left, h), count_bin(xs, left)) && Nat.is_eq(lookup(right, h), count_bin(xs, right))

def pairs_with(left: Nat, +right: Nat) -> Bool:
  match left:
    case 0n:
      pair_ok(0n, right)
    case 1n++p:
      pair_ok(1n+p, right) && pairs_with(p, right)

def all_pairs(right: Nat) -> Bool:
  match right:
    case 0n:
      pairs_with(64n, 0n)
    case 1n++p:
      pairs_with(64n, 1n+p) && all_pairs(p)
