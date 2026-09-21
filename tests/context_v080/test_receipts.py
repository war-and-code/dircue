#!/usr/bin/env python3
"""Tamper tests for the independent 0.8 receipt verifier."""

from __future__ import annotations

import copy
import json
from pathlib import Path
import tempfile
import unittest

import common
import verify


class ReceiptTests(unittest.TestCase):
    def test_raw_record_rejects_tampered_bytes_and_ambiguous_encoding(self) -> None:
        record = common.recorded((1, b"out", b"err"))
        self.assertEqual(verify.verify_raw(record), (1, b"out", b"err"))
        changed = copy.deepcopy(record); changed["stdout"] = "changed"
        with self.assertRaises(AssertionError): verify.verify_raw(changed)
        ambiguous = copy.deepcopy(record); ambiguous["stdout_base64"] = "b3V0"
        with self.assertRaises(AssertionError): verify.verify_raw(ambiguous)

    def test_compatibility_recomputes_raw_equality_and_build_binding(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); baseline=root/"old"; candidate=root/"new"; build_path=root/"build.json"; receipt_path=root/"receipt.json"
            baseline.write_bytes(b"old"); candidate.write_bytes(b"new"); build={"schema":"dircue-context-v080-build-1"}; build_path.write_text(json.dumps(build))
            raw=common.recorded((0,b"same",b""))
            receipt={"schema":"test-schema","passed":True,"total":1,"exact_matches":1,"baseline_release":"v0.7.0","baseline_sha256":common.sha256(baseline),"candidate_sha256":common.sha256(candidate),"build_receipt":build,"build_receipt_sha256":common.sha256(build_path),"cases":[{"id":"one","equal":True,"baseline":raw,"candidate":copy.deepcopy(raw)}]}
            receipt_path.write_text(json.dumps(receipt)); verify.verify_compatibility(receipt_path,"test-schema",1,baseline,candidate,build,build_path)
            receipt["cases"][0]["candidate"]["exit"]=1; receipt_path.write_text(json.dumps(receipt))
            with self.assertRaises(AssertionError): verify.verify_compatibility(receipt_path,"test-schema",1,baseline,candidate,build,build_path)

    def test_build_manifest_checks_current_compilation_input(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); candidate=root/"candidate"; candidate.write_bytes(b"binary")
            source=verify.build.source_state()
            receipt={"schema":"dircue-context-v080-build-1","candidate_sha256":common.sha256(candidate),"source_at_build":source,"files":{"go.mod":common.sha256(common.ROOT/"go.mod")}}
            path=root/"build.json"; path.write_text(json.dumps(receipt)); verify.verify_build(path,candidate,require_current=False)
            receipt["files"]["go.mod"]="0"*64; path.write_text(json.dumps(receipt))
            with self.assertRaises(AssertionError): verify.verify_build(path,candidate,require_current=False)

    def test_performance_summary_and_claim_limit_are_recomputed(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); baseline=root/"old"; candidate=root/"new"; build_path=root/"build.json"; receipt_path=root/"perf.json"
            baseline.write_bytes(b"old"); candidate.write_bytes(b"new"); build={"schema":"build"}; build_path.write_text(json.dumps(build))
            samples=[{"seconds":float(i+1),"peak_rss_bytes":100+i} for i in range(5)]
            summary=common.sample_summary(samples)
            receipt={"schema":"dircue-context-v080-performance-1","passed":True,"baseline_sha256":common.sha256(baseline),"candidate_sha256":common.sha256(candidate),"build_receipt":build,"build_receipt_sha256":common.sha256(build_path),"driver_sha256":common.sha256(Path(verify.__file__).with_name("performance.py")),"shared_helper_sha256":common.sha256(Path(common.__file__)),"method":{"repetitions":5,"warmups":1,"claim_limit":"staged and full workflows answer different questions; ratios are recorded observations, not equivalent-output savings"},"summaries":{"lane":summary},"identical_output":{"corpus_languages":True,"xml_languages":True}}
            receipt_path.write_text(json.dumps(receipt)); verify.verify_performance(receipt_path,baseline,candidate,build,build_path)
            receipt["summaries"]["lane"]["median_seconds"]=999; receipt_path.write_text(json.dumps(receipt))
            with self.assertRaises(AssertionError): verify.verify_performance(receipt_path,baseline,candidate,build,build_path)


if __name__ == "__main__": unittest.main()
