import Base
import ./model.bend as M
import ./LAWS.bend as Laws

def add_successor(a: Nat, -b: Nat) -> {1n+Nat.add(a, b) == Nat.add(a, 1n+b) : Nat}:
  match a:
    case 0n:
      {==}
    case 1n+p:
      %add_successor(p, b) : {2n+Nat.add(p, b) == 1n+_ : Nat}
      {==}

def Laws.zero_total(n):
  match n:
    case 0n:
      {==}
    case 1n+p:
      Laws.zero_total(p)

def Laws.bump_total(index, h):
  match index h:
    case 0n M.Last{count}:
      {==}
    case 0n M.Cell{count, rest}:
      {==}
    case 1n+p M.Last{count}:
      {==}
    case 1n+p M.Cell{+count, +rest}:
      %Laws.bump_total(p, rest) : {1n+Nat.add(count, M.total(rest)) == Nat.add(count, _) : Nat}
      add_successor(count, M.total(rest))

def Laws.aggregate_total(xs, h):
  match xs:
    case Nil{}:
      {==}
    case Con{+x, +tail}:
      %Laws.aggregate_total(tail, M.bump(x, h)) : {1n+Nat.add(M.length(tail), M.total(h)) == _ : Nat}
      %Laws.bump_total(x, h) : {1n+Nat.add(M.length(tail), M.total(h)) == Nat.add(M.length(tail), _) : Nat}
      add_successor(M.length(tail), M.total(h))

def Laws.streaming_partitions(xs, ys, h):
  match xs:
    case Nil{}:
      {==}
    case Con{x, tail}:
      Laws.streaming_partitions(tail, ys, M.bump(x, h))

def increment_add(b: Bool, -x: Nat, -y: Nat) -> {Nat.add(M.increment(b, x), y) == M.increment(b, Nat.add(x, y)) : Nat}:
  match b:
    case True{}:
      {==}
    case False{}:
      {==}

def Laws.independent_partition_counts(xs, ys, index):
  match xs:
    case Nil{}:
      {==}
    case Con{+x, +tail}:
      %Laws.independent_partition_counts(tail, ys, index) : {Nat.add(M.increment(Nat.is_eq(x, index), M.count_bin(tail, index)), M.count_bin(ys, index)) == M.increment(Nat.is_eq(x, index), _) : Nat}
      increment_add(Nat.is_eq(x, index), M.count_bin(tail, index), M.count_bin(ys, index))

def Laws.admitted_pair_certificate():
  {==}
