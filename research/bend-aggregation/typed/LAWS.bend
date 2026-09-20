import Base
import ./model.bend as M

law merge_zero:
  for n: Nat
  for b: M.Histogram(n)
  {M.merge(n, M.zero(n), b) == b : M.Histogram(n)}

law merge_bump:
  for +n: Nat
  for +index: M.Index(n)
  for +a: M.Histogram(n)
  for +b: M.Histogram(n)
  {M.merge(n, M.bump(n, index, a), b) == M.bump(n, index, M.merge(n, a, b)) : M.Histogram(n)}

law aggregate_merge:
  for +n: Nat
  for +xs: +List<M.Index(n)>
  for +a: M.Histogram(n)
  for +b: M.Histogram(n)
  {M.merge(n, M.aggregate(n, xs, a), b) == M.aggregate(n, xs, M.merge(n, a, b)) : M.Histogram(n)}

law merge_commutative:
  for +n: Nat
  for +a: M.Histogram(n)
  for +b: M.Histogram(n)
  {M.merge(n, a, b) == M.merge(n, b, a) : M.Histogram(n)}

law streaming_partitions:
  for +n: Nat
  for +xs: +List<M.Index(n)>
  for +ys: +List<M.Index(n)>
  for +h: M.Histogram(n)
  {M.aggregate(n, ys, M.aggregate(n, xs, h)) == M.aggregate(n, M.append(M.Index(n), xs, ys), h) : M.Histogram(n)}

# LAW: Two independently initialized, equally sized histograms merged cell by
# cell equal a single histogram of their concatenated input. Index(n) admits
# only indices 0..n-1, including when n is the production width65.
law independent_partition_merge:
  for +n: Nat
  for +xs: +List<M.Index(n)>
  for +ys: +List<M.Index(n)>
  {M.merge(n, M.aggregate(n, xs, M.zero(n)), M.aggregate(n, ys, M.zero(n))) == M.aggregate(n, M.append(M.Index(n), xs, ys), M.zero(n)) : M.Histogram(n)}

law zero_total:
  for n: Nat
  {0n == M.total(n, M.zero(n)) : Nat}

law bump_total:
  for +n: Nat
  for +index: M.Index(n)
  for +h: M.Histogram(n)
  {1n+M.total(n, h) == M.total(n, M.bump(n, index, h)) : Nat}

law aggregate_total:
  for +n: Nat
  for +xs: +List<M.Index(n)>
  for +h: M.Histogram(n)
  {Nat.add(M.length(M.Index(n), xs), M.total(n, h)) == M.total(n, M.aggregate(n, xs, h)) : Nat}

# LAW: Converting any admitted index to a natural and back preserves it.
law admitted_index_round_trip:
  for +n: Nat
  for +index: M.Index(n)
  {Some{index} == M.from_nat(n, M.index_nat(n, index)) : Maybe<&2, M.Index(n)>}

# LAW: Updating an admitted bin adds one exactly at that index. Every other
# admitted bin retains its previous count. Conservation alone cannot show this.
law lookup_update:
  for +n: Nat
  for +updated: M.Index(n)
  for +queried: M.Index(n)
  for +h: M.Histogram(n)
  {M.increment(M.index_equal(n, updated, queried), M.lookup(n, queried, h)) == M.lookup(n, queried, M.bump(n, updated, h)) : Nat}

# LAW: Every natural at or beyond the histogram width is rejected by admission.
law rejects_out_of_range:
  for +n: Nat
  for +extra: Nat
  {None{} == M.from_nat(n, Nat.add(n, extra)) : Maybe<&2, M.Index(n)>}

# LAW: Admission preserves the order and bin identity of every list encoded
# from admitted indices. This connects the list adapter to index conversion.
law admitted_list_round_trip:
  for +n: Nat
  for +xs: +List<M.Index(n)>
  {Some{xs} == M.admit(n, M.encode_indices(n, xs)) : Maybe<&2, +List<M.Index(n)>>}
