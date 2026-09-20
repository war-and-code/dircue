import Base
import ./model.bend as R
import ../model.bend as M

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

def histogram_tail(n: Nat, h: R.Histogram(n)) -> String:
  match n:
    case 0n:
      ""
    case 1n+p:
      match h:
        case R.More{count, rest}:
          "," ++ Nat.show(count) ++ histogram_tail(p, rest)

def histogram(h: R.Histogram(65n)) -> String:
  match h:
    case R.More{count, rest}:
      "[" ++ Nat.show(count) ++ histogram_tail(64n, rest) ++ "]"

def report_admitted(+values: +List<U32>, +bs: +List<Nat>, admitted: Maybe<&2, +List<R.Index(65n)>>) -> String:
  match admitted:
    case None{}:
      "{\"error\":\"invalid bin index\"}"
    case Some{+indices}:
      +h = R.aggregate(65n, indices, R.zero(65n))
      +partition = R.merge(65n, R.aggregate(65n, R.take(R.Index(65n), 7n, indices), R.zero(65n)), R.aggregate(65n, R.drop(R.Index(65n), 7n, indices), R.zero(65n)))
      "{\"values\":" ++ values_list(values) ++ ",\"bins\":" ++ nat_list(bs) ++ ",\"histogram\":" ++ histogram(h) ++ ",\"merged_histogram\":" ++ histogram(partition) ++ ",\"count\":" ++ Nat.show(R.total(65n, h)) ++ "}"

def report(+values: +List<U32>) -> String:
  +bs = M.bins(values)
  report_admitted(values, bs, R.admit(65n, bs))

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

def rejects(value: Maybe<&2, R.Index(65n)>) -> Bool:
  match value:
    case None{}:
      True{}
    case Some{index}:
      False{}

def bin_range(count: Nat, +start: Nat) -> +List<Nat>:
  match count:
    case 0n:
      Nil{}
    case 1n+p:
      Con{start, bin_range(p, 1n+start)}

def all_bins(admitted: Maybe<&2, +List<R.Index(65n)>>) -> String:
  match admitted:
    case None{}:
      "null"
    case Some{indices}:
      histogram(R.aggregate(65n, indices, R.zero(65n)))

def singleton_matrix(count: Nat, +start: Nat) -> String:
  match count:
    case 0n:
      ""
    case 1n+p:
      "," ++ all_bins(R.admit(65n, [start])) ++ singleton_matrix(p, 1n+start)

def main() -> IO(Unit):
  IO.print("{\"cases\":[" ++ report([]) ++ "," ++ report([0]) ++ "," ++ report([1]) ++ "," ++ report([0, 1, 2, 3, 4, 7, 8, 15, 16, 31, 32, 63, 64, 127, 128, 255, 256, 257, 65535, 65536, 2147483647, 2147483648, 4294967295]) ++ "," ++ report(range(257n, 0)) ++ "," ++ report(repeated(257n, 7)) ++ "," ++ report(range(17n, 4294967279)) ++ "],\"admitted_pair_certificate\":" ++ truth(M.all_pairs(64n)) ++ ",\"out_of_range_rejected\":" ++ truth(rejects(R.from_nat(65n, 65n))) ++ ",\"all_indices_histogram\":" ++ all_bins(R.admit(65n, bin_range(65n, 0n))) ++ ",\"singleton_histograms\":[" ++ all_bins(R.admit(65n, [0n])) ++ singleton_matrix(64n, 1n) ++ "]" ++ "}")
