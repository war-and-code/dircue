import Base

type More<-T: Data> is Data:
  More{count: Nat, rest: T}

def Histogram(n: Nat) -> Data:
  match n:
    case 0n:
      Unit
    case 1n+p:
      More<Histogram(p)>

type MoreIndex<-T: Data> is Data:
  First{}
  Next{index: T}

def Index(n: Nat) -> Data:
  match n:
    case 0n:
      Empty
    case 1n+p:
      MoreIndex<Index(p)>

def zero(n: Nat) -> Histogram(n):
  match n:
    case 0n:
      Unit{}
    case 1n+p:
      More{0n, zero(p)}

def total(n: Nat, h: Histogram(n)) -> Nat:
  match n:
    case 0n:
      0n
    case 1n+p:
      match h:
        case More{count, rest}:
          Nat.add(count, total(p, rest))

def bump(n: Nat, index: Index(n), h: Histogram(n)) -> Histogram(n):
  match n:
    case 0n:
      match index:
    case 1n+p:
      match index h:
        case First{} More{count, rest}:
          More{1n+count, rest}
        case Next{index} More{count, rest}:
          More{count, bump(p, index, rest)}

def merge(n: Nat, a: Histogram(n), b: Histogram(n)) -> Histogram(n):
  match n:
    case 0n:
      Unit{}
    case 1n+p:
      match a b:
        case More{ac, ar} More{bc, br}:
          More{Nat.add(ac, bc), merge(p, ar, br)}

def aggregate(+n: Nat, xs: +List<Index(n)>, h: Histogram(n)) -> Histogram(n):
  match xs:
    case Nil{}:
      h
    case Con{x, tail}:
      aggregate(n, tail, bump(n, x, h))

def append(-T: Data, xs: +List<T>, ys: +List<T>) -> +List<T>:
  match xs:
    case Nil{}:
      ys
    case Con{x, tail}:
      Con{x, append(T, tail, ys)}

def length(-T: Data, xs: +List<T>) -> Nat:
  match xs:
    case Nil{}:
      0n
    case Con{x, tail}:
      1n+length(T, tail)

# Admission is separate from aggregation: invalid indices have no Index(n)
# value and are rejected, never clamped or silently dropped.
def next_index(-n: Nat, value: Maybe<&2, Index(n)>) -> Maybe<&2, Index(1n+n)>:
  match value:
    case None{}:
      None{}
    case Some{index}:
      Some{Next{index}}

def from_nat(n: Nat, value: Nat) -> Maybe<&2, Index(n)>:
  match n:
    case 0n:
      None{}
    case 1n+p:
      match value:
        case 0n:
          Some{First{}}
        case 1n+v:
          next_index(p, from_nat(p, v))

def cons_index(-n: Nat, head: Maybe<&2, Index(n)>, tail: Maybe<&2, +List<Index(n)>>) -> Maybe<&2, +List<Index(n)>>:
  match head tail:
    case None{} tail:
      None{}
    case Some{x} None{}:
      None{}
    case Some{x} Some{xs}:
      Some{Con{x, xs}}

def admit(+n: Nat, xs: +List<Nat>) -> Maybe<&2, +List<Index(n)>>:
  match xs:
    case Nil{}:
      Some{Nil{}}
    case Con{x, rest}:
      cons_index(n, from_nat(n, x), admit(n, rest))

def take(-T: Data, n: Nat, xs: +List<T>) -> +List<T>:
  match n xs:
    case 0n xs:
      Nil{}
    case 1n+p Nil{}:
      Nil{}
    case 1n+p Con{x, rest}:
      Con{x, take(T, p, rest)}

def drop(-T: Data, n: Nat, xs: +List<T>) -> +List<T>:
  match n xs:
    case 0n xs:
      xs
    case 1n+p Nil{}:
      Nil{}
    case 1n+p Con{x, rest}:
      drop(T, p, rest)

def index_nat(n: Nat, index: Index(n)) -> Nat:
  match n:
    case 0n:
      match index:
    case 1n+p:
      match index:
        case First{}:
          0n
        case Next{i}:
          1n+index_nat(p, i)

def increment(matches: Bool, count: Nat) -> Nat:
  match matches:
    case True{}:
      1n+count
    case False{}:
      count

def index_equal(n: Nat, a: Index(n), b: Index(n)) -> Bool:
  match n:
    case 0n:
      match a:
    case 1n+p:
      match a b:
        case First{} First{}:
          True{}
        case First{} Next{b}:
          False{}
        case Next{a} First{}:
          False{}
        case Next{a} Next{b}:
          index_equal(p, a, b)

def lookup(n: Nat, index: Index(n), h: Histogram(n)) -> Nat:
  match n:
    case 0n:
      match index:
    case 1n+p:
      match index h:
        case First{} More{count, rest}:
          count
        case Next{i} More{count, rest}:
          lookup(p, i, rest)

def encode_indices(+n: Nat, xs: +List<Index(n)>) -> +List<Nat>:
  match xs:
    case Nil{}:
      Nil{}
    case Con{index, rest}:
      Con{index_nat(n, index), encode_indices(n, rest)}
