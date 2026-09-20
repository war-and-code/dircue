# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HigherOrderCO
# Modified for dircue: entry ordering, bounded retention, and partition proofs.
# Adapts bendlang/bend demos/proof_insertion_sort at revision
# 981899d6b2fb2c109ed83545cffed9d20910a025. See THIRD_PARTY_NOTICES.md.

import Base
import ./model.bend as M
import ./LAWS.bend as Laws

def take_shorter(k: Nat, xs: +List<M.Entry>) -> {M.take(k, xs) == M.take(k, M.take(1n+k, xs)) : +List<M.Entry>}:
  match k xs:
    case 0n xs:
      {==}
    case 1n+p Nil{}:
      {==}
    case 1n+p Con{head, tail}:
      %take_shorter(p, tail) : {Con{head, M.take(p, tail)} == Con{head, _} : +List<M.Entry>}
      {==}

def take_head(k: Nat, head: M.Entry, tail: +List<M.Entry>) -> {M.take(k, Con{head, tail}) == M.take(k, Con{head, M.take(k, tail)}) : +List<M.Entry>}:
  match k:
    case 0n:
      {==}
    case 1n+p:
      %take_shorter(p, tail) : {Con{head, M.take(p, tail)} == Con{head, _} : +List<M.Entry>}
      {==}

def insertion_step(+p: Nat, +x: M.Entry, +head: M.Entry, +tail: +List<M.Entry>, before: Bool,
  ih: {M.take(p, M.insert(tail, x)) == M.take(p, M.insert(M.take(p, tail), x)) : +List<M.Entry>})
  -> {M.take(1n+p, M.choose(before, x, head, tail, M.insert(tail, x))) == M.take(1n+p, M.choose(before, x, head, M.take(p, tail), M.insert(M.take(p, tail), x))) : +List<M.Entry>}:
  match before:
    case True{}:
      %take_head(p, head, tail) : {Con{x, M.take(p, Con{head, tail})} == Con{x, _} : +List<M.Entry>}
      {==}
    case False{}:
      %ih : {Con{head, M.take(p, M.insert(tail, x))} == Con{head, _} : +List<M.Entry>}
      {==}

def Laws.insertion_prefix(k, xs, x):
  match k xs:
    case 0n xs:
      {==}
    case 1n+p Nil{}:
      {==}
    case 1n+p Con{head, tail}:
      insertion_step(p, x, head, tail, M.better(x, head), Laws.insertion_prefix(p, tail, x))

def Laws.bounded_matches_full(k, xs, state):
  match xs:
    case Nil{}:
      {==}
    case Con{x, tail}:
      %Laws.insertion_prefix(k, state, x) : {M.take(k, M.full(tail, M.insert(state, x))) == M.bounded(k, tail, _) : +List<M.Entry>}
      Laws.bounded_matches_full(k, tail, M.insert(state, x))

def ids_order(ai: Nat, bi: Nat) -> {M.decision_bool(M.LE(ai, bi), M.LE(bi, ai), M.decide_ids(ai, bi)) == Nat.is_lt(ai, bi) : Bool}:
  match ai bi:
    case 0n 0n:
      {==}
    case 0n 1n+b:
      {==}
    case 1n+a 0n:
      {==}
    case 1n+a 1n+b:
      ids_order(a, b)

def values_order(av: Nat, bv: Nat, ai: Nat, bi: Nat) -> {M.decision_bool(M.Before_values(av, bv, ai, bi), M.Before_values(bv, av, bi, ai), M.decide_values(av, bv, ai, bi)) == (Nat.is_gt(av, bv) || (Nat.is_eq(av, bv) && Nat.is_lt(ai, bi))) : Bool}:
  match av bv:
    case 0n 0n:
      ids_order(ai, bi)
    case 0n 1n+b:
      {==}
    case 1n+a 0n:
      {==}
    case 1n+a 1n+b:
      values_order(a, b, ai, bi)

def Laws.comparator_order(a, b):
  match a b:
    case M.Entry{av, ai} M.Entry{bv, bi}:
      values_order(av, bv, ai, bi)

def from_insert_step(-lo: M.Entry, -x: M.Entry, -head: M.Entry, -tail: +List<M.Entry>,
  sorted: M.Sorted_from(lo, Con{head, tail}), lower: M.Before(lo, x),
  rec: M.Sorted_from(head, tail) -> M.Before(head, x) -> M.Sorted_from(head, M.insert(tail, x)),
  choice: Or(M.Before(x, head), M.Before(head, x)))
  -> M.Sorted_from(lo, M.choose(M.decision_bool(M.Before(x, head), M.Before(head, x), choice), x, head, tail, M.insert(tail, x))):
  (before, rest) = sorted
  match choice:
    case Inl{proof}:
      (lower, proof, rest)
    case Inr{proof}:
      (before, rec(rest, proof))

def from_insert(xs: +List<M.Entry>, -lo: M.Entry, +x: M.Entry) -> M.Sorted_from(lo, xs) -> M.Before(lo, x) -> M.Sorted_from(lo, M.insert(xs, x)):
  match xs:
    case Nil{}:
      sorted => lower => (lower, Unit{})
    case Con{+head, +tail}:
      sorted => lower => (
        %Laws.comparator_order(x, head) : M.Sorted_from(lo, M.choose(_, x, head, tail, M.insert(tail, x)))
        from_insert_step(lo, x, head, tail, sorted, lower, from_insert(tail, head, x), M.decide(x, head))
      )

def insert_ordered_step(+x: M.Entry, +head: M.Entry, +tail: +List<M.Entry>, sorted: M.Sorted_from(head, tail),
  choice: Or(M.Before(x, head), M.Before(head, x)))
  -> M.Sorted(M.choose(M.decision_bool(M.Before(x, head), M.Before(head, x), choice), x, head, tail, M.insert(tail, x))):
  match choice:
    case Inl{proof}:
      (proof, sorted)
    case Inr{proof}:
      from_insert(tail, head, x)(sorted, proof)

def insert_ordered(+xs: +List<M.Entry>, +x: M.Entry, sorted: M.Sorted(xs)) -> M.Sorted(M.insert(xs, x)):
  match xs:
    case Nil{}:
      Unit{}
    case Con{head, tail}:
      %Laws.comparator_order(x, head) : M.Sorted(M.choose(_, x, head, tail, M.insert(tail, x)))
      insert_ordered_step(x, head, tail, sorted, M.decide(x, head))

def full_ordered_from(xs: +List<M.Entry>, +state: +List<M.Entry>, sorted: M.Sorted(state)) -> M.Sorted(M.full(xs, state)):
  match xs:
    case Nil{}:
      sorted
    case Con{+x, tail}:
      full_ordered_from(tail, M.insert(state, x), insert_ordered(state, x, sorted))

def Laws.full_ordered(xs):
  full_ordered_from(xs, Nil{}, Unit{})

def take_from(k: Nat, -lo: M.Entry, xs: +List<M.Entry>, sorted: M.Sorted_from(lo, xs)) -> M.Sorted_from(lo, M.take(k, xs)):
  match k xs:
    case 0n xs:
      Unit{}
    case 1n+p Nil{}:
      Unit{}
    case 1n+p Con{head, tail}:
      (before, rest) = sorted
      (before, take_from(p, head, tail, rest))

def take_ordered(k: Nat, xs: +List<M.Entry>, sorted: M.Sorted(xs)) -> M.Sorted(M.take(k, xs)):
  match k xs:
    case 0n xs:
      Unit{}
    case 1n+p Nil{}:
      Unit{}
    case 1n+p Con{head, tail}:
      take_from(p, head, tail, sorted)

def take_empty(k: Nat) -> {M.take(k, Nil{}) == Nil{} : +List<M.Entry>}:
  match k:
    case 0n:
      {==}
    case 1n+p:
      {==}

def Laws.bounded_ordered(k, xs):
  %take_empty(k) : M.Sorted(M.bounded(k, xs, _))
  %Laws.bounded_matches_full(k, xs, Nil{}) : M.Sorted(_)
  take_ordered(k, M.full(xs, Nil{}), Laws.full_ordered(xs))

def increment_comm(a: Bool, b: Bool, -n: Nat) -> {M.increment(a, M.increment(b, n)) == M.increment(b, M.increment(a, n)) : Nat}:
  match a b:
    case True{} True{}:
      {==}
    case True{} False{}:
      {==}
    case False{} True{}:
      {==}
    case False{} False{}:
      {==}

def insert_counts_step(+x: M.Entry, +query: M.Entry, +head: M.Entry, +tail: +List<M.Entry>, before: Bool,
  ih: {M.increment(M.same_entry(x, query), M.count(tail, query)) == M.count(M.insert(tail, x), query) : Nat})
  -> {M.increment(M.same_entry(x, query), M.count(Con{head, tail}, query)) == M.count(M.choose(before, x, head, tail, M.insert(tail, x)), query) : Nat}:
  match before:
    case True{}:
      {==}
    case False{}:
      %ih : {M.increment(M.same_entry(x, query), M.increment(M.same_entry(head, query), M.count(tail, query))) == M.increment(M.same_entry(head, query), _) : Nat}
      increment_comm(M.same_entry(x, query), M.same_entry(head, query), M.count(tail, query))

def insert_counts(+xs: +List<M.Entry>, +x: M.Entry, +query: M.Entry) -> {M.increment(M.same_entry(x, query), M.count(xs, query)) == M.count(M.insert(xs, x), query) : Nat}:
  match xs:
    case Nil{}:
      {==}
    case Con{head, tail}:
      insert_counts_step(x, query, head, tail, M.better(x, head), insert_counts(tail, x, query))

def add_successor(a: Nat, -b: Nat) -> {1n+Nat.add(a, b) == Nat.add(a, 1n+b) : Nat}:
  match a:
    case 0n:
      {==}
    case 1n+p:
      %add_successor(p, b) : {2n+Nat.add(p, b) == 1n+_ : Nat}
      {==}

def increment_add(b: Bool, a: Nat, -c: Nat) -> {Nat.add(M.increment(b, a), c) == Nat.add(a, M.increment(b, c)) : Nat}:
  match b:
    case True{}:
      add_successor(a, c)
    case False{}:
      {==}

def full_counts_from(+xs: +List<M.Entry>, +state: +List<M.Entry>, +query: M.Entry)
  -> {Nat.add(M.count(xs, query), M.count(state, query)) == M.count(M.full(xs, state), query) : Nat}:
  match xs:
    case Nil{}:
      {==}
    case Con{x, tail}:
      %full_counts_from(tail, M.insert(state, x), query) : {Nat.add(M.increment(M.same_entry(x, query), M.count(tail, query)), M.count(state, query)) == _ : Nat}
      %insert_counts(state, x, query) : {Nat.add(M.increment(M.same_entry(x, query), M.count(tail, query)), M.count(state, query)) == Nat.add(M.count(tail, query), _) : Nat}
      increment_add(M.same_entry(x, query), M.count(tail, query), M.count(state, query))

def add_zero(a: Nat) -> {Nat.add(a, 0n) == a : Nat}:
  match a:
    case 0n:
      {==}
    case 1n+p:
      %add_zero(p) : {1n+Nat.add(p, 0n) == 1n+_ : Nat}
      {==}

def Laws.full_population(xs, query):
  %add_zero(M.count(xs, query)) : {_ == M.count(M.full(xs, Nil{}), query) : Nat}
  full_counts_from(xs, Nil{}, query)

def take_same(k: Nat, xs: +List<M.Entry>) -> {M.take(k, xs) == M.take(k, M.take(k, xs)) : +List<M.Entry>}:
  match k xs:
    case 0n xs:
      {==}
    case 1n+p Nil{}:
      {==}
    case 1n+p Con{head, tail}:
      %take_same(p, tail) : {Con{head, M.take(p, tail)} == Con{head, _} : +List<M.Entry>}
      {==}

def merge_left_step(+p: Nat, +x: M.Entry, +xt: +List<M.Entry>, +y: M.Entry, +yt: +List<M.Entry>, before: Bool,
  left: {M.take(p, M.merge(xt, Con{y, yt})) == M.take(p, M.merge(M.take(p, xt), Con{y, yt})) : +List<M.Entry>},
  right: {M.take(p, M.merge(Con{x, xt}, yt)) == M.take(p, M.merge(M.take(p, Con{x, xt}), yt)) : +List<M.Entry>},
  pruned: {M.take(p, M.merge(Con{x, M.take(p, xt)}, yt)) == M.take(p, M.merge(M.take(p, Con{x, M.take(p, xt)}), yt)) : +List<M.Entry>})
  -> {M.take(1n+p, M.merge_choice(before, x, y, u => M.merge(xt, Con{y, yt}), u => M.merge(Con{x, xt}, yt))) == M.take(1n+p, M.merge_choice(before, x, y, u => M.merge(M.take(p, xt), Con{y, yt}), u => M.merge(Con{x, M.take(p, xt)}, yt))) : +List<M.Entry>}:
  match before:
    case True{}:
      %left : {Con{x, M.take(p, M.merge(xt, Con{y, yt}))} == Con{x, _} : +List<M.Entry>}
      {==}
    case False{}:
      %Equal.sym(+List<M.Entry>, M.take(p, M.merge(Con{x, M.take(p, xt)}, yt)), M.take(p, M.merge(M.take(p, Con{x, M.take(p, xt)}), yt)), pruned) : {Con{y, M.take(p, M.merge(Con{x, xt}, yt))} == Con{y, _} : +List<M.Entry>}
      %take_head(p, x, xt) : {Con{y, M.take(p, M.merge(Con{x, xt}, yt))} == Con{y, M.take(p, M.merge(_, yt))} : +List<M.Entry>}
      %right : {Con{y, M.take(p, M.merge(Con{x, xt}, yt))} == Con{y, _} : +List<M.Entry>}
      {==}

def Laws.merge_left_prefix(k, xs, ys):
  match k xs ys:
    case 0n xs ys:
      {==}
    case 1n+p Nil{} ys:
      {==}
    case 1n+p Con{x, xt} Nil{}:
      take_same(1n+p, Con{x, xt})
    case 1n+p Con{x, xt} Con{y, yt}:
      merge_left_step(p, x, xt, y, yt, M.better(x, y), Laws.merge_left_prefix(p, xt, Con{y, yt}), Laws.merge_left_prefix(p, Con{x, xt}, yt), Laws.merge_left_prefix(p, Con{x, M.take(p, xt)}, yt))

def merge_right_step(+p: Nat, +x: M.Entry, +xt: +List<M.Entry>, +y: M.Entry, +yt: +List<M.Entry>, before: Bool,
  left: {M.take(p, M.merge(xt, Con{y, yt})) == M.take(p, M.merge(xt, M.take(p, Con{y, yt}))) : +List<M.Entry>},
  pruned: {M.take(p, M.merge(xt, Con{y, M.take(p, yt)})) == M.take(p, M.merge(xt, M.take(p, Con{y, M.take(p, yt)}))) : +List<M.Entry>},
  right: {M.take(p, M.merge(Con{x, xt}, yt)) == M.take(p, M.merge(Con{x, xt}, M.take(p, yt))) : +List<M.Entry>})
  -> {M.take(1n+p, M.merge_choice(before, x, y, u => M.merge(xt, Con{y, yt}), u => M.merge(Con{x, xt}, yt))) == M.take(1n+p, M.merge_choice(before, x, y, u => M.merge(xt, Con{y, M.take(p, yt)}), u => M.merge(Con{x, xt}, M.take(p, yt)))) : +List<M.Entry>}:
  match before:
    case True{}:
      %Equal.sym(+List<M.Entry>, M.take(p, M.merge(xt, Con{y, M.take(p, yt)})), M.take(p, M.merge(xt, M.take(p, Con{y, M.take(p, yt)}))), pruned) : {Con{x, M.take(p, M.merge(xt, Con{y, yt}))} == Con{x, _} : +List<M.Entry>}
      %take_head(p, y, yt) : {Con{x, M.take(p, M.merge(xt, Con{y, yt}))} == Con{x, M.take(p, M.merge(xt, _))} : +List<M.Entry>}
      %left : {Con{x, M.take(p, M.merge(xt, Con{y, yt}))} == Con{x, _} : +List<M.Entry>}
      {==}
    case False{}:
      %right : {Con{y, M.take(p, M.merge(Con{x, xt}, yt))} == Con{y, _} : +List<M.Entry>}
      {==}

def Laws.merge_right_prefix(k, xs, ys):
  match k xs ys:
    case 0n xs ys:
      {==}
    case 1n+p Nil{} ys:
      take_same(1n+p, ys)
    case 1n+p Con{x, xt} Nil{}:
      {==}
    case 1n+p Con{x, xt} Con{y, yt}:
      merge_right_step(p, x, xt, y, yt, M.better(x, y), Laws.merge_right_prefix(p, xt, Con{y, yt}), Laws.merge_right_prefix(p, xt, Con{y, M.take(p, yt)}), Laws.merge_right_prefix(p, Con{x, xt}, yt))

def Laws.independent_merge_prefix(k, xs, ys):
  %Laws.merge_right_prefix(k, M.take(k, xs), ys) : {M.take(k, M.merge(xs, ys)) == _ : +List<M.Entry>}
  Laws.merge_left_prefix(k, xs, ys)

def from_merge_step(-lo: M.Entry, -x: M.Entry, -xt: +List<M.Entry>, -y: M.Entry, -yt: +List<M.Entry>,
  sx: M.Sorted_from(lo, Con{x, xt}), sy: M.Sorted_from(lo, Con{y, yt}),
  left: M.Sorted_from(x, xt) -> M.Sorted_from(x, Con{y, yt}) -> M.Sorted_from(x, M.merge(xt, Con{y, yt})),
  right: M.Sorted_from(y, Con{x, xt}) -> M.Sorted_from(y, yt) -> M.Sorted_from(y, M.merge(Con{x, xt}, yt)),
  choice: Or(M.Before(x, y), M.Before(y, x)))
  -> M.Sorted_from(lo, M.merge_choice(M.decision_bool(M.Before(x, y), M.Before(y, x), choice), x, y, u => M.merge(xt, Con{y, yt}), u => M.merge(Con{x, xt}, yt))):
  (lx, tx) = sx
  (ly, ty) = sy
  match choice:
    case Inl{proof}:
      (lx, left(tx, (proof, ty)))
    case Inr{proof}:
      (ly, right((proof, tx), ty))

def from_merge(+xs: +List<M.Entry>, +ys: +List<M.Entry>, -lo: M.Entry) -> M.Sorted_from(lo, xs) -> M.Sorted_from(lo, ys) -> M.Sorted_from(lo, M.merge(xs, ys)):
  match xs ys:
    case Nil{} ys:
      sx => sy => sy
    case Con{x, xt} Nil{}:
      sx => sy => sx
    case Con{x, xt} Con{y, yt}:
      sx => sy => (
        %Laws.comparator_order(x, y) : M.Sorted_from(lo, M.merge_choice(_, x, y, u => M.merge(xt, ys), u => M.merge(xs, yt)))
        from_merge_step(lo, x, xt, y, yt, sx, sy, from_merge(xt, ys, x), from_merge(xs, yt, y), M.decide(x, y))
      )

def merge_ordered_step(+x: M.Entry, +xt: +List<M.Entry>, +y: M.Entry, +yt: +List<M.Entry>, sx: M.Sorted_from(x, xt), sy: M.Sorted_from(y, yt),
  choice: Or(M.Before(x, y), M.Before(y, x)))
  -> M.Sorted(M.merge_choice(M.decision_bool(M.Before(x, y), M.Before(y, x), choice), x, y, u => M.merge(xt, Con{y, yt}), u => M.merge(Con{x, xt}, yt))):
  match choice:
    case Inl{proof}:
      from_merge(xt, Con{y, yt}, x)(sx, (proof, sy))
    case Inr{proof}:
      from_merge(Con{x, xt}, yt, y)((proof, sx), sy)

def Laws.merge_ordered(xs, ys, sorted_xs, sorted_ys):
  match xs ys:
    case Nil{} ys:
      sorted_ys
    case Con{x, xt} Nil{}:
      sorted_xs
    case Con{x, xt} Con{y, yt}:
      %Laws.comparator_order(x, y) : M.Sorted(M.merge_choice(_, x, y, u => M.merge(xt, ys), u => M.merge(xs, yt)))
      merge_ordered_step(x, xt, y, yt, sorted_xs, sorted_ys, M.decide(x, y))

def increment_left_add(b: Bool, -a: Nat, -c: Nat) -> {Nat.add(M.increment(b, a), c) == M.increment(b, Nat.add(a, c)) : Nat}:
  match b:
    case True{}:
      {==}
    case False{}:
      {==}

def increment_right_add(b: Bool, a: Nat, -c: Nat) -> {Nat.add(a, M.increment(b, c)) == M.increment(b, Nat.add(a, c)) : Nat}:
  match b:
    case True{}:
      %add_successor(a, c) : {_ == 1n+Nat.add(a, c) : Nat}
      {==}
    case False{}:
      {==}

def merge_count_step(+x: M.Entry, +xt: +List<M.Entry>, +y: M.Entry, +yt: +List<M.Entry>, +query: M.Entry, before: Bool,
  left: {Nat.add(M.count(xt, query), M.count(Con{y, yt}, query)) == M.count(M.merge(xt, Con{y, yt}), query) : Nat},
  right: {Nat.add(M.count(Con{x, xt}, query), M.count(yt, query)) == M.count(M.merge(Con{x, xt}, yt), query) : Nat})
  -> {Nat.add(M.count(Con{x, xt}, query), M.count(Con{y, yt}, query)) == M.count(M.merge_choice(before, x, y, u => M.merge(xt, Con{y, yt}), u => M.merge(Con{x, xt}, yt)), query) : Nat}:
  match before:
    case True{}:
      %left : {Nat.add(M.increment(M.same_entry(x, query), M.count(xt, query)), M.count(Con{y, yt}, query)) == M.increment(M.same_entry(x, query), _) : Nat}
      increment_left_add(M.same_entry(x, query), M.count(xt, query), M.count(Con{y, yt}, query))
    case False{}:
      %right : {Nat.add(M.count(Con{x, xt}, query), M.increment(M.same_entry(y, query), M.count(yt, query))) == M.increment(M.same_entry(y, query), _) : Nat}
      increment_right_add(M.same_entry(y, query), M.count(Con{x, xt}, query), M.count(yt, query))

def Laws.merge_population(xs, ys, query):
  match xs ys:
    case Nil{} ys:
      {==}
    case Con{x, xt} Nil{}:
      add_zero(M.count(xs, query))
    case Con{x, xt} Con{y, yt}:
      merge_count_step(x, xt, y, yt, query, M.better(x, y), Laws.merge_population(xt, ys, query), Laws.merge_population(xs, yt, query))

def bounded_empty_eq(+k: Nat, +xs: +List<M.Entry>) -> {M.take(k, M.full(xs, Nil{})) == M.bounded(k, xs, Nil{}) : +List<M.Entry>}:
  %take_empty(k) : {M.take(k, M.full(xs, Nil{})) == M.bounded(k, xs, _) : +List<M.Entry>}
  Laws.bounded_matches_full(k, xs, Nil{})

def Laws.independently_bounded_partitions(k, xs, ys):
  %bounded_empty_eq(k, xs) : {M.take(k, M.merge(M.full(xs, Nil{}), M.full(ys, Nil{}))) == M.take(k, M.merge(_, M.bounded(k, ys, Nil{}))) : +List<M.Entry>}
  %bounded_empty_eq(k, ys) : {M.take(k, M.merge(M.full(xs, Nil{}), M.full(ys, Nil{}))) == M.take(k, M.merge(M.take(k, M.full(xs, Nil{})), _)) : +List<M.Entry>}
  Laws.independent_merge_prefix(k, M.full(xs, Nil{}), M.full(ys, Nil{}))

def Laws.partition_fold_prefix(k, parts, state):
  match parts:
    case Nil{}:
      {==}
    case Con{part, rest}:
      %bounded_empty_eq(k, part) : {M.take(k, M.all_full(rest, M.merge(state, M.full(part, Nil{})))) == M.all_bounded(k, rest, M.take(k, M.merge(M.take(k, state), _))) : +List<M.Entry>}
      %Laws.independent_merge_prefix(k, state, M.full(part, Nil{})) : {M.take(k, M.all_full(rest, M.merge(state, M.full(part, Nil{})))) == M.all_bounded(k, rest, _) : +List<M.Entry>}
      Laws.partition_fold_prefix(k, rest, M.merge(state, M.full(part, Nil{})))

def Laws.many_bounded_partitions(k, parts):
  %take_empty(k) : {M.take(k, M.all_full(parts, Nil{})) == M.all_bounded(k, parts, _) : +List<M.Entry>}
  Laws.partition_fold_prefix(k, parts, Nil{})

def Laws.many_ordered(parts, state, sorted):
  match parts:
    case Nil{}:
      sorted
    case Con{part, rest}:
      Laws.many_ordered(rest, M.merge(state, M.full(part, Nil{})), Laws.merge_ordered(state, M.full(part, Nil{}), sorted, Laws.full_ordered(part)))

def Laws.many_population(parts, state, query):
  match parts:
    case Nil{}:
      {==}
    case Con{part, rest}:
      %Laws.many_population(rest, M.merge(state, M.full(part, Nil{})), query) : {M.population(rest, query, Nat.add(M.count(state, query), M.count(part, query))) == _ : Nat}
      %Laws.merge_population(state, M.full(part, Nil{}), query) : {M.population(rest, query, Nat.add(M.count(state, query), M.count(part, query))) == M.population(rest, query, _) : Nat}
      %Laws.full_population(part, query) : {M.population(rest, query, Nat.add(M.count(state, query), M.count(part, query))) == M.population(rest, query, Nat.add(M.count(state, query), _)) : Nat}
      {==}
