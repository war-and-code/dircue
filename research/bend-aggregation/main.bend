import Base
import ./model.bend as M

def nat_tail(xs: +List<Nat>) -> String:
  match xs:
    case Nil{}:
      ""
    case Con{x, rest}:
      "," ++ Nat.show(x) ++ nat_tail(rest)

def nat_list(xs: +List<Nat>) -> String:
  match xs:
    case Nil{}:
      "[]"
    case Con{x, rest}:
      "[" ++ Nat.show(x) ++ nat_tail(rest) ++ "]"

def values_tail(xs: +List<U32>) -> String:
  match xs:
    case Nil{}:
      ""
    case Con{x, rest}:
      "," ++ U32.show(x) ++ values_tail(rest)

def values_list(xs: +List<U32>) -> String:
  match xs:
    case Nil{}:
      "[]"
    case Con{x, rest}:
      "[" ++ U32.show(x) ++ values_tail(rest) ++ "]"

def histogram_tail(h: M.Histogram) -> String:
  match h:
    case M.Last{count}:
      Nat.show(count)
    case M.Cell{count, rest}:
      Nat.show(count) ++ "," ++ histogram_tail(rest)

def report(+values: +List<U32>) -> String:
  +bs = M.bins(values)
  +h = M.aggregate(bs, M.zero(64n))
  "{\"values\":" ++ values_list(values) ++ ",\"bins\":" ++ nat_list(bs) ++ ",\"histogram\":[" ++ histogram_tail(h) ++ "],\"count\":" ++ Nat.show(M.total(h)) ++ "}"

def range(count: Nat, +start: U32) -> +List<U32>:
  match count:
    case 0n:
      Nil{}
    case 1n+p:
      Con{start, range(p, (start + 1 : U32))}

def repeated(count: Nat, +value: U32) -> +List<U32>:
  match count:
    case 0n:
      Nil{}
    case 1n+p:
      Con{value, repeated(p, value)}

def truth(value: Bool) -> String:
  match value:
    case True{}:
      "true"
    case False{}:
      "false"

def main() -> IO(Unit):
  IO.print("{\"cases\":[" ++ report([]) ++ "," ++ report([0]) ++ "," ++ report([1]) ++ "," ++ report([0, 1, 2, 3, 4, 7, 8, 15, 16, 31, 32, 63, 64, 127, 128, 255, 256, 257, 65535, 65536, 2147483647, 2147483648, 4294967295]) ++ "," ++ report(range(257n, 0)) ++ "," ++ report(repeated(257n, 7)) ++ "," ++ report(range(17n, 4294967279)) ++ "],\"admitted_pair_certificate\":" ++ truth(M.all_pairs(64n)) ++ "}")
