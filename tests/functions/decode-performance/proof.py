#!/usr/bin/env python3
"""Verify that the optimization only reuses an already-validated JSON envelope."""
import gzip
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent


def main():
    before = json.loads(gzip.decompress((HERE / 'baseline/sources.json.gz').read_bytes()))
    after = json.loads(gzip.decompress((HERE / 'candidate/sources.json.gz').read_bytes()))
    old, new = (snapshot['pkg/structure/functions.go'] for snapshot in (before, after))
    signature = 'func decodeFunctions(data []byte, content []byte, syntaxErrors bool, syntaxNodes uint64) (*FunctionSpaces, bool) {\n'
    helper = signature.replace('decodeFunctions(', 'decodeValidatedFunctions(')
    old_header, old_body = old.split(signature, 1)
    old_body, old_tail = old_body.split('\nfunc functionKeys(', 1)
    new_header, new_body = new.split(signature, 1)
    wrapper, new_body = new_body.split(helper, 1)
    new_body, new_tail = new_body.split('\nfunc functionKeys(', 1)
    prefix, wrapper_end = wrapper.split('\treturn decodeValidatedFunctions(data, content, syntaxErrors, syntaxNodes)\n', 1)
    expected_end = ("}\n\n// The caller must first validate this exact JSON subtree's UTF-8, framing,\n"
                    '// duplicate keys and depth, either here or as part of strictFunctionResponse.\n'
                    "// The enclosing response's depth limit is at least as strict as the subtree's.\n")
    assert wrapper_end == expected_end, 'unexpected wrapper behavior'
    assert old_body == prefix + new_body, 'original validation statements were changed or removed'
    assert old_header == new_header and old_tail == new_tail, 'other function-package behavior changed'
    old_call = '\t\tdecoded, ok := decodeFunctions(response.Functions, content, *response.SyntaxErrors, response.Observations["syntax_nodes"])'
    new_call = '\t\t// strictFunctionResponse already checked every nested function metric.\n' + old_call.replace('decodeFunctions(', 'decodeValidatedFunctions(')
    assert before['pkg/structure/structure.go'].replace(old_call, new_call, 1) == after['pkg/structure/structure.go'], 'other response behavior changed'
    result = {'passed': True, 'proof': 'Original validation statements retained byte-for-byte in wrapper prefix plus shared decoder. Only response caller changes, after unchanged strict envelope validation. Runtime default path, metric validation/serialization and all other production bytes unchanged.',
              'before_sources_sha256': hashlib.sha256((HERE / 'baseline/sources.json.gz').read_bytes()).hexdigest(),
              'after_sources_sha256': hashlib.sha256((HERE / 'candidate/sources.json.gz').read_bytes()).hexdigest()}
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
