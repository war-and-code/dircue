import Base
import ./model.bend as M

def entry(x: M.Entry) -> String:
  match x:
    case M.Entry{value, identity}:
      "{\"value\":" ++ Nat.show(value) ++ ",\"identity\":" ++ Nat.show(identity) ++ "}"

def tail(xs: +List<M.Entry>) -> String:
  match xs:
    case Nil{}:
      ""
    case Con{x, rest}:
      "," ++ entry(x) ++ tail(rest)

def entries(xs: +List<M.Entry>) -> String:
  match xs:
    case Nil{}:
      "[]"
    case Con{x, rest}:
      "[" ++ entry(x) ++ tail(rest) ++ "]"

def varied(n: Nat, +identity: Nat) -> +List<M.Entry>:
  match n:
    case 0n:
      Nil{}
    case 1n+p:
      Con{M.Entry{Nat.mod(Nat.mul(identity, 17n), 13n), identity}, varied(p, 1n+identity)}

def ties(n: Nat) -> +List<M.Entry>:
  match n:
    case 0n:
      Nil{}
    case 1n++p:
      Con{M.Entry{42n, p}, ties(p)}

def report(+k: Nat, +xs: +List<M.Entry>) -> String:
  "{\"limit\":" ++ Nat.show(k) ++ ",\"entries\":" ++ entries(xs) ++ ",\"bounded\":" ++ entries(M.bounded(k, xs, [])) ++ ",\"full_prefix\":" ++ entries(M.take(k, M.full(xs, []))) ++ ",\"partitioned\":" ++ entries(M.take(k, M.merge(M.bounded(k, M.take(17n, xs), []), M.bounded(k, M.drop(17n, xs), [])))) ++ ",\"many_partitioned\":" ++ entries(M.all_bounded(k, [M.take(17n, xs), M.take(17n, M.drop(17n, xs)), M.drop(34n, xs)], [])) ++ "}"

def reports(+xs: +List<M.Entry>) -> String:
  report(0n, xs) ++ "," ++ report(1n, xs) ++ "," ++ report(10n, xs) ++ "," ++ report(129n, xs)

def main() -> IO(Unit):
  IO.print("{\"topk_cases\":[" ++ reports(varied(128n, 0n)) ++ "," ++ reports(ties(37n)) ++ "," ++ reports([]) ++ "]}")
