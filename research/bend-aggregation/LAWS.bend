import Base
import ./model.bend as M

# LAW: Initializing a nonempty histogram gives a total count of zero.
law zero_total:
  for n: Nat
  {0n == M.total(M.zero(n)) : Nat}

# LAW: One admitted observation adds exactly one to the total. The model's
# saturation also preserves this identity outside the admitted index domain.
law bump_total:
  for index: Nat
  for h: M.Histogram
  {1n+M.total(h) == M.total(M.bump(index, h)) : Nat}

# LAW: Aggregation preserves every observation in its population count.
law aggregate_total:
  for +xs: +List<Nat>
  for +h: M.Histogram
  {Nat.add(M.length(xs), M.total(h)) == M.total(M.aggregate(xs, h)) : Nat}

# LAW: Processing two consecutive partitions is equal to processing their
# concatenation. This law concerns streaming state, not independent partials.
law streaming_partitions:
  for +xs: +List<Nat>
  for +ys: +List<Nat>
  for +h: M.Histogram
  {M.aggregate(ys, M.aggregate(xs, h)) == M.aggregate(M.append(xs, ys), h) : M.Histogram}

# LAW: For every bin, adding counts computed independently from two partitions
# is equal to counting that bin in their concatenation.
law independent_partition_counts:
  for +xs: +List<Nat>
  for +ys: +List<Nat>
  for +index: Nat
  {Nat.add(M.count_bin(xs, index), M.count_bin(ys, index)) == M.count_bin(M.append(xs, ys), index) : Nat}

# LAW: Every ordered pair of admitted bin indices agrees with direct counting
# in the populated bins. This is a finite 65*65 certificate, not refinement for
# lists of arbitrary length or a proof about production Rust/Go code.
law admitted_pair_certificate:
  {M.all_pairs(64n) == True{} : Bool}
