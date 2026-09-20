import Base
import ./model.bend as M

# LAW: The retained prefix of one insertion depends only on the old retained
# prefix. This holds even when the starting list is not sorted.
law insertion_prefix:
  for +k: Nat
  for +xs: +List<M.Entry>
  for +x: M.Entry
  {M.take(k, M.insert(xs, x)) == M.take(k, M.insert(M.take(k, xs), x)) : +List<M.Entry>}

# LAW: Incremental bounded retention matches truncating the unbounded
# insertion fold, for every limit, input list, and initial state.
law bounded_matches_full:
  for +k: Nat
  for +xs: +List<M.Entry>
  for +state: +List<M.Entry>
  {M.take(k, M.full(xs, state)) == M.bounded(k, xs, M.take(k, state)) : +List<M.Entry>}

# LAW: The executable comparator agrees with a decision carrying evidence
# of the declared descending-value / ascending-identity order.
law comparator_order:
  for +a: M.Entry
  for +b: M.Entry
  {M.decision_bool(M.Before(a, b), M.Before(b, a), M.decide(a, b)) == M.better(a, b) : Bool}

# LAW: Starting empty, the unbounded insertion fold is canonically ordered.
law full_ordered:
  for +xs: +List<M.Entry>
  M.Sorted(M.full(xs, Nil{}))

# LAW: Starting empty, bounded retention is canonically ordered for every K.
law bounded_ordered:
  for +k: Nat
  for +xs: +List<M.Entry>
  M.Sorted(M.bounded(k, xs, Nil{}))

# LAW: The unbounded reference preserves the multiplicity of every full entry.
# Together with order, this rules out a sorted reference that drops inputs.
law full_population:
  for +xs: +List<M.Entry>
  for +query: M.Entry
  {M.count(xs, query) == M.count(M.full(xs, Nil{}), query) : Nat}

law merge_left_prefix:
  for +k: Nat
  for +xs: +List<M.Entry>
  for +ys: +List<M.Entry>
  {M.take(k, M.merge(xs, ys)) == M.take(k, M.merge(M.take(k, xs), ys)) : +List<M.Entry>}

law merge_right_prefix:
  for +k: Nat
  for +xs: +List<M.Entry>
  for +ys: +List<M.Entry>
  {M.take(k, M.merge(xs, ys)) == M.take(k, M.merge(xs, M.take(k, ys))) : +List<M.Entry>}

# LAW: Merging retained prefixes selects exactly the same prefix as merging
# both complete streams. The order of each stream is preserved by merge.
law independent_merge_prefix:
  for +k: Nat
  for +xs: +List<M.Entry>
  for +ys: +List<M.Entry>
  {M.take(k, M.merge(xs, ys)) == M.take(k, M.merge(M.take(k, xs), M.take(k, ys))) : +List<M.Entry>}

law merge_ordered:
  for +xs: +List<M.Entry>
  for +ys: +List<M.Entry>
  for sorted_xs: M.Sorted(xs)
  for sorted_ys: M.Sorted(ys)
  M.Sorted(M.merge(xs, ys))

law merge_population:
  for +xs: +List<M.Entry>
  for +ys: +List<M.Entry>
  for +query: M.Entry
  {Nat.add(M.count(xs, query), M.count(ys, query)) == M.count(M.merge(xs, ys), query) : Nat}

# LAW: Merging separately bounded local selections equals selecting from the
# complete merged populations. full_ordered/full_population and merge_ordered/
# merge_population specify the reference as ordered and population-preserving.
law independently_bounded_partitions:
  for +k: Nat
  for +xs: +List<M.Entry>
  for +ys: +List<M.Entry>
  {M.take(k, M.merge(M.full(xs, Nil{}), M.full(ys, Nil{}))) == M.take(k, M.merge(M.bounded(k, xs, Nil{}), M.bounded(k, ys, Nil{}))) : +List<M.Entry>}

law partition_fold_prefix:
  for +k: Nat
  for +parts: +List<+List<M.Entry>>
  for +state: +List<M.Entry>
  {M.take(k, M.all_full(parts, state)) == M.all_bounded(k, parts, M.take(k, state)) : +List<M.Entry>}

# LAW: The repeated merger retains exactly the global reference prefix for
# every number of partitions, including empty partitions and empty input.
law many_bounded_partitions:
  for +k: Nat
  for +parts: +List<+List<M.Entry>>
  {M.take(k, M.all_full(parts, Nil{})) == M.all_bounded(k, parts, Nil{}) : +List<M.Entry>}

law many_ordered:
  for +parts: +List<+List<M.Entry>>
  for +state: +List<M.Entry>
  for sorted: M.Sorted(state)
  M.Sorted(M.all_full(parts, state))

law many_population:
  for +parts: +List<+List<M.Entry>>
  for +state: +List<M.Entry>
  for +query: M.Entry
  {M.population(parts, query, M.count(state, query)) == M.count(M.all_full(parts, state), query) : Nat}
