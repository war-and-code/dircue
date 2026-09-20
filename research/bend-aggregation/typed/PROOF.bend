import Base
import ./model.bend as M
import ./LAWS.bend as Laws

def Laws.merge_zero(n, b):
  match n:
    case 0n:
      match b:
        case Unit{}:
          {==}
    case 1n+p:
      match b:
        case M.More{count, rest}:
          %Laws.merge_zero(p, rest) : {M.More{count, M.merge(p, M.zero(p), rest)} == M.More{count, _} : M.Histogram(1n+p)}
          {==}

def Laws.merge_bump(n, index, a, b):
  match n:
    case 0n:
      match index:
    case 1n+p:
      match index a b:
        case M.First{} M.More{ac, ar} M.More{bc, br}:
          {==}
        case M.Next{i} M.More{ac, ar} M.More{bc, br}:
          %Laws.merge_bump(p, i, ar, br) : {M.More{Nat.add(ac, bc), M.merge(p, M.bump(p, i, ar), br)} == M.More{Nat.add(ac, bc), _} : M.Histogram(1n+p)}
          {==}

def Laws.aggregate_merge(n, xs, a, b):
  match xs:
    case Nil{}:
      {==}
    case Con{x, tail}:
      %Laws.merge_bump(n, x, a, b) : {M.merge(n, M.aggregate(n, tail, M.bump(n, x, a)), b) == M.aggregate(n, tail, _) : M.Histogram(n)}
      Laws.aggregate_merge(n, tail, M.bump(n, x, a), b)

def add_zero(a: Nat) -> {Nat.add(a, 0n) == a : Nat}:
  match a:
    case 0n:
      {==}
    case 1n+p:
      %add_zero(p) : {1n+Nat.add(p, 0n) == 1n+_ : Nat}
      {==}

def add_successor(a: Nat, -b: Nat) -> {1n+Nat.add(a, b) == Nat.add(a, 1n+b) : Nat}:
  match a:
    case 0n:
      {==}
    case 1n+p:
      %add_successor(p, b) : {2n+Nat.add(p, b) == 1n+_ : Nat}
      {==}

def add_commutative(a: Nat, +b: Nat) -> {Nat.add(a, b) == Nat.add(b, a) : Nat}:
  match a:
    case 0n:
      %add_zero(b) : {_ == Nat.add(b, 0n) : Nat}
      {==}
    case 1n+p:
      %add_successor(b, p) : {1n+Nat.add(p, b) == _ : Nat}
      %add_commutative(p, b) : {1n+Nat.add(p, b) == 1n+_ : Nat}
      {==}

def Laws.merge_commutative(n, a, b):
  match n:
    case 0n:
      {==}
    case 1n+p:
      match a b:
        case M.More{ac, ar} M.More{bc, br}:
          %Laws.merge_commutative(p, ar, br) : {M.More{Nat.add(ac, bc), M.merge(p, ar, br)} == M.More{Nat.add(bc, ac), _} : M.Histogram(1n+p)}
          %add_commutative(ac, bc) : {M.More{Nat.add(ac, bc), M.merge(p, ar, br)} == M.More{_, M.merge(p, ar, br)} : M.Histogram(1n+p)}
          {==}

def Laws.streaming_partitions(n, xs, ys, h):
  match xs:
    case Nil{}:
      {==}
    case Con{x, tail}:
      Laws.streaming_partitions(n, tail, ys, M.bump(n, x, h))

def Laws.independent_partition_merge(n, xs, ys):
  %Laws.streaming_partitions(n, xs, ys, M.zero(n)) : {M.merge(n, M.aggregate(n, xs, M.zero(n)), M.aggregate(n, ys, M.zero(n))) == _ : M.Histogram(n)}
  %Laws.merge_commutative(n, M.aggregate(n, ys, M.zero(n)), M.aggregate(n, xs, M.zero(n))) : {_ == M.aggregate(n, ys, M.aggregate(n, xs, M.zero(n))) : M.Histogram(n)}
  %Laws.merge_zero(n, M.aggregate(n, xs, M.zero(n))) : {M.merge(n, M.aggregate(n, ys, M.zero(n)), M.aggregate(n, xs, M.zero(n))) == M.aggregate(n, ys, _) : M.Histogram(n)}
  Laws.aggregate_merge(n, ys, M.zero(n), M.aggregate(n, xs, M.zero(n)))

def Laws.zero_total(n):
  match n:
    case 0n:
      {==}
    case 1n+p:
      Laws.zero_total(p)

def Laws.bump_total(n, index, h):
  match n:
    case 0n:
      match index:
    case 1n+p:
      match index h:
        case M.First{} M.More{count, rest}:
          {==}
        case M.Next{i} M.More{count, rest}:
          %Laws.bump_total(p, i, rest) : {1n+Nat.add(count, M.total(p, rest)) == Nat.add(count, _) : Nat}
          add_successor(count, M.total(p, rest))

def Laws.aggregate_total(n, xs, h):
  match xs:
    case Nil{}:
      {==}
    case Con{x, tail}:
      %Laws.aggregate_total(n, tail, M.bump(n, x, h)) : {1n+Nat.add(M.length(M.Index(n), tail), M.total(n, h)) == _ : Nat}
      %Laws.bump_total(n, x, h) : {1n+Nat.add(M.length(M.Index(n), tail), M.total(n, h)) == Nat.add(M.length(M.Index(n), tail), _) : Nat}
      add_successor(M.length(M.Index(n), tail), M.total(n, h))

def Laws.admitted_index_round_trip(n, index):
  match n:
    case 0n:
      match index:
    case 1n+p:
      match index:
        case M.First{}:
          {==}
        case M.Next{i}:
          %Laws.admitted_index_round_trip(p, i) : {Some{M.Next{i}} == M.next_index(p, _) : Maybe<&2, M.Index(1n+p)>}
          {==}

def Laws.lookup_update(n, updated, queried, h):
  match n:
    case 0n:
      match updated:
    case 1n+p:
      match updated queried h:
        case M.First{} M.First{} M.More{count, rest}:
          {==}
        case M.First{} M.Next{j} M.More{count, rest}:
          {==}
        case M.Next{i} M.First{} M.More{count, rest}:
          {==}
        case M.Next{i} M.Next{j} M.More{count, rest}:
          Laws.lookup_update(p, i, j, rest)

def Laws.rejects_out_of_range(n, extra):
  match n:
    case 0n:
      {==}
    case 1n+p:
      %Laws.rejects_out_of_range(p, extra) : {None{} == M.next_index(p, _) : Maybe<&2, M.Index(1n+p)>}
      {==}

def Laws.admitted_list_round_trip(n, xs):
  match xs:
    case Nil{}:
      {==}
    case Con{index, rest}:
      %Laws.admitted_index_round_trip(n, index) : {Some{Con{index, rest}} == M.cons_index(n, _, M.admit(n, M.encode_indices(n, rest))) : Maybe<&2, +List<M.Index(n)>>}
      %Laws.admitted_list_round_trip(n, rest) : {Some{Con{index, rest}} == M.cons_index(n, Some{index}, _) : Maybe<&2, +List<M.Index(n)>>}
      {==}
