# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HigherOrderCO
# Modified for dircue: entry ordering, bounded retention, and partition merging.
# Adapts bendlang/bend demos/proof_insertion_sort at revision
# 981899d6b2fb2c109ed83545cffed9d20910a025. See THIRD_PARTY_NOTICES.md.

import Base

type Entry is Data:
  Entry{value: Nat, identity: Nat}

def better(a: Entry, b: Entry) -> Bool:
  match a b:
    case Entry{+av, ai} Entry{+bv, bi}:
      Nat.is_gt(av, bv) || (Nat.is_eq(av, bv) && Nat.is_lt(ai, bi))

def choose(before: Bool, x: Entry, head: Entry, tail: +List<Entry>, recursive: +List<Entry>) -> +List<Entry>:
  match before:
    case True{}:
      Con{x, Con{head, tail}}
    case False{}:
      Con{head, recursive}

def insert(xs: +List<Entry>, +x: Entry) -> +List<Entry>:
  match xs:
    case Nil{}:
      Con{x, Nil{}}
    case Con{+head, +tail}:
      choose(better(x, head), x, head, tail, insert(tail, x))

def take(k: Nat, xs: +List<Entry>) -> +List<Entry>:
  match k xs:
    case 0n xs:
      Nil{}
    case 1n+p Nil{}:
      Nil{}
    case 1n+p Con{head, tail}:
      Con{head, take(p, tail)}

def full(xs: +List<Entry>, state: +List<Entry>) -> +List<Entry>:
  match xs:
    case Nil{}:
      state
    case Con{x, tail}:
      full(tail, insert(state, x))

def bounded(+k: Nat, xs: +List<Entry>, state: +List<Entry>) -> +List<Entry>:
  match xs:
    case Nil{}:
      state
    case Con{x, tail}:
      bounded(k, tail, take(k, insert(state, x)))

# Canonical order: larger values precede smaller ones; identities ascend
# within equal values. Before permits equality for repeated identical entries.
def LE(a: Nat, b: Nat) -> Data:
  match a b:
    case 0n b:
      Unit
    case 1n+a 0n:
      Empty
    case 1n+a 1n+b:
      LE(a, b)

def Before_values(av: Nat, bv: Nat, ai: Nat, bi: Nat) -> Data:
  match av bv:
    case 0n 0n:
      LE(ai, bi)
    case 0n 1n+b:
      Empty
    case 1n+a 0n:
      Unit
    case 1n+a 1n+b:
      Before_values(a, b, ai, bi)

def Before(a: Entry, b: Entry) -> Data:
  match a b:
    case Entry{av, ai} Entry{bv, bi}:
      Before_values(av, bv, ai, bi)

def decide_ids(ai: Nat, bi: Nat) -> Or(LE(ai, bi), LE(bi, ai)):
  match ai bi:
    case 0n 0n:
      Inr{Unit{}}
    case 0n 1n+b:
      Inl{Unit{}}
    case 1n+a 0n:
      Inr{Unit{}}
    case 1n+a 1n+b:
      decide_ids(a, b)

def decide_values(av: Nat, bv: Nat, ai: Nat, bi: Nat) -> Or(Before_values(av, bv, ai, bi), Before_values(bv, av, bi, ai)):
  match av bv:
    case 0n 0n:
      decide_ids(ai, bi)
    case 0n 1n+b:
      Inr{Unit{}}
    case 1n+a 0n:
      Inl{Unit{}}
    case 1n+a 1n+b:
      decide_values(a, b, ai, bi)

def decide(a: Entry, b: Entry) -> Or(Before(a, b), Before(b, a)):
  match a b:
    case Entry{av, ai} Entry{bv, bi}:
      decide_values(av, bv, ai, bi)

def decision_bool(-A: Type, -B: Type, value: Or(A, B)) -> Bool:
  match value:
    case Inl{proof}:
      True{}
    case Inr{proof}:
      False{}

def Sorted_from(lo: Entry, xs: +List<Entry>) -> Type:
  match xs:
    case Nil{}:
      Unit
    case Con{+head, tail}:
      Before(lo, head) & Sorted_from(head, tail)

def Sorted(xs: +List<Entry>) -> Type:
  match xs:
    case Nil{}:
      Unit
    case Con{head, tail}:
      Sorted_from(head, tail)

def same_entry(a: Entry, b: Entry) -> Bool:
  match a b:
    case Entry{av, ai} Entry{bv, bi}:
      Nat.is_eq(av, bv) && Nat.is_eq(ai, bi)

def increment(matches: Bool, count: Nat) -> Nat:
  match matches:
    case True{}:
      1n+count
    case False{}:
      count

def count(xs: +List<Entry>, +query: Entry) -> Nat:
  match xs:
    case Nil{}:
      0n
    case Con{x, rest}:
      increment(same_entry(x, query), count(rest, query))

# Full merge is a separate operation over the ordered local populations.
def merge_choice(before: Bool, x: Entry, y: Entry, left: Unit -> +List<Entry>, right: Unit -> +List<Entry>) -> +List<Entry>:
  match before:
    case True{}:
      Con{x, left(Unit{})}
    case False{}:
      Con{y, right(Unit{})}

def merge(+xs: +List<Entry>, +ys: +List<Entry>) -> +List<Entry>:
  match xs ys:
    case Nil{} ys:
      ys
    case Con{x, xt} Nil{}:
      Con{x, xt}
    case Con{x, xt} Con{y, yt}:
      merge_choice(better(x, y), x, y, u => merge(xt, ys), u => merge(xs, yt))

def drop(k: Nat, xs: +List<Entry>) -> +List<Entry>:
  match k xs:
    case 0n xs:
      xs
    case 1n+p Nil{}:
      Nil{}
    case 1n+p Con{head, tail}:
      drop(p, tail)

def all_full(parts: +List<+List<Entry>>, state: +List<Entry>) -> +List<Entry>:
  match parts:
    case Nil{}:
      state
    case Con{part, rest}:
      all_full(rest, merge(state, full(part, Nil{})))

def all_bounded(+k: Nat, parts: +List<+List<Entry>>, state: +List<Entry>) -> +List<Entry>:
  match parts:
    case Nil{}:
      state
    case Con{part, rest}:
      all_bounded(k, rest, take(k, merge(state, bounded(k, part, Nil{}))))

# An input-only population fold; it never calls a sorting or merging function.
def population(parts: +List<+List<Entry>>, +query: Entry, starting: Nat) -> Nat:
  match parts:
    case Nil{}:
      starting
    case Con{part, rest}:
      population(rest, query, Nat.add(starting, count(part, query)))
