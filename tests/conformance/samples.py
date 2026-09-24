#!/usr/bin/env python3
"""Exploratory classifier differential on every pinned upstream Linguist sample.
# Result keys retain the original evidence format across the project rename.

This records discrepancies rather than asserting that Enry equals Ruby for every
language. Directory aggregation and CLI conformance belong to run.py.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
ARCHIVE_URL = 'https://codeload.github.com/github-linguist/linguist/tar.gz/refs/tags/v9.7.0'
ARCHIVE_SHA = 'e7b85d06f5e61a810303b8d2e03fc199760525c079fef4dd6f8b7c86342234d9'
GIT_REF = 'e0c78d62c42abae6122235d8e68a7aa43eef89da'


def run(argv, cwd=None):
    p = subprocess.run([str(a) for a in argv], cwd=cwd, env=dict(os.environ, GOWORK='off'), capture_output=True, text=True)
    if p.returncode:
        raise RuntimeError(f'{argv}: {p.stderr}')
    return p.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--require-match', action='store_true', help='Fail if any Dircue label or ordered token sequence differs from Ruby')
    parser.add_argument('--archive', type=Path, help='Use a previously downloaded pinned archive; hash always verified')
    parser.add_argument('--image', default='dircue-linguist:9.7.0')
    parser.add_argument('--output', type=Path, default=HERE/'results'/'samples.json')
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='dircue-samples-') as directory:
        work = Path(directory)
        archive = args.archive or work/'samples.tar.gz'
        if not args.archive:
            print('Downloading pinned upstream sample corpus', flush=True)
            urllib.request.urlretrieve(ARCHIVE_URL, archive)
        if hashlib.sha256(archive.read_bytes()).hexdigest()!=ARCHIVE_SHA:
            raise RuntimeError('upstream archive SHA-256 mismatch')
        samples = work/'samples'
        samples.mkdir()
        with tarfile.open(archive, 'r:gz') as tar:
            prefix = 'linguist-9.7.0/samples/'
            for member in tar:
                if not member.name.startswith(prefix) or not member.isfile():
                    continue
                relative = Path(member.name[len(prefix):])
                if relative.is_absolute() or '..' in relative.parts:
                    raise RuntimeError('unsafe sample archive member')
                target = samples/relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(tar.extractfile(member).read())
        probe = work/'classifier-probe'
        run(['go', 'build', '-trimpath', '-o', probe, './tests/conformance/classifier'], ROOT)
        baseline_dir = work/'official-enry-baseline'
        baseline_dir.mkdir()
        (baseline_dir/'go.mod').write_text('module official-enry-baseline\n\ngo 1.26.6\n\nrequire github.com/go-enry/go-enry/v2 v2.9.6\n')
        (baseline_dir/'main.go').write_bytes((HERE/'enry_baseline.go.txt').read_bytes())
        baseline = work/'official-enry-probe'
        run(['go', 'build', '-mod=mod', '-trimpath', '-o', baseline, '.'], baseline_dir)
        baseline_module = json.loads(run(['go','list','-m','-json','github.com/go-enry/go-enry/v2'], baseline_dir))
        if baseline_module.get('Replace') or baseline_module.get('Sum') != 'h1:np63eOtMV56zfYDHnFVgpEVOk8fr2kmylcMnAZUDbSs=':
            raise RuntimeError('official Enry baseline was replaced or has unexpected source hash')
        print('Classifying all samples with independent official Enry and the Dircue maintained Enry classifier', flush=True)
        official = json.loads(run([baseline, samples]))
        candidates = json.loads(run([probe, samples]))
        token_dir = work/'tokenizer-probe'
        token_dir.mkdir()
        # The maintained Enry is embedded in the dircue module. A probe module
        # path under third_party/go-enry may import its internal tokenizer.
        embedded = 'github.com/war-and-code/dircue/third_party/go-enry'
        (token_dir/'go.mod').write_text('module '+embedded+'/conformancetokenizer\n\ngo 1.26.6\n\nrequire github.com/war-and-code/dircue v0.0.0\nreplace github.com/war-and-code/dircue => '+str(ROOT)+'\n')
        (token_dir/'main.go').write_bytes((HERE/'tokenizer_probe.go.txt').read_bytes().replace(b'github.com/go-enry/go-enry/v2/internal/tokenizer', (embedded+'/internal/tokenizer').encode()))
        token_probe = work/'tokenizer-probe-bin'
        run(['go','build','-mod=mod','-trimpath','-o',token_probe,'.'],token_dir)
        token_results = json.loads(run([token_probe,samples]))
        print(f'Classifying {len(candidates)} identical samples with Ruby Linguist', flush=True)
        ruby = '''require 'linguist'; require 'json'; require 'digest'; root='/samples'; result={}; Dir.glob(root+'/**/*',File::FNM_DOTMATCH).sort.each do |path|; next unless File.file?(path); begin; b=Linguist::FileBlob.new(path,root); tokens=Linguist::Tokenizer.tokenize(File.binread(path)); result[path.delete_prefix(root+'/')]={language:b.language&.name || '',language_type:b.language&.type&.to_s || '',binary:b.binary?,token_hash:Digest::SHA256.hexdigest(tokens.join("\\0")),token_count:tokens.length}; rescue => e; result[path.delete_prefix(root+'/')]={error:e.class.to_s+': '+e.message}; end; end; puts JSON.generate(result)'''
        reference = json.loads(run(['docker','run','--rm','--network=none','-v',str(samples)+':/samples:ro',args.image,'ruby','-e',ruby]))
        if set(reference)!=set(candidates) or set(reference)!=set(official) or set(reference)!=set(token_results):
            raise RuntimeError('reference and candidate did not enumerate the same files')
        results=[]
        for path in sorted(candidates):
            expected=reference[path]
            actual=dict(candidates[path])
            actual['refreshed_enry'] = actual['enry']
            actual['enry'] = official[path]
            actual['tokenizer'] = token_results[path]
            actual['tokens_match'] = token_results[path]['hash'] == expected.get('token_hash') and token_results[path]['count'] == expected.get('token_count')
            results.append(dict(path=path,reference=expected,**actual,enry_matches=actual['enry']==expected.get('language'),auragaze_matches=actual['auragaze']==expected.get('language')))
        summary={'samples':len(results), 'sample_language_directories':len({x['path'].split('/')[0] for x in results}),
                 'reference_errors':sum('error' in x['reference'] for x in results), 'enry_matches':sum(x['enry_matches'] for x in results),
                 'auragaze_matches':sum(x['auragaze_matches'] for x in results),
                 'token_sequences_match':sum(x['tokens_match'] for x in results),
                 'programming_markup_samples':sum(x['reference'].get('language_type') in ('programming','markup') for x in results),
                 'programming_markup_matches':sum(x['auragaze_matches'] and x['reference'].get('language_type') in ('programming','markup') for x in results)}
        report={'scope':'Exploratory per-file classification, full identical content, all pinned upstream samples; not repository inclusion or universal correctness.',
                'provenance':{'archive_url':ARCHIVE_URL,'archive_sha256':ARCHIVE_SHA,'upstream_git_ref':GIT_REF,
                              'image_id':run(['docker','image','inspect',args.image,'--format','{{.Id}}']).strip(),
                              'go_version':run(['go','version']).strip(),'go_enry_module':'github.com/war-and-code/dircue/third_party/go-enry (embedded, see third_party/go-enry/PROVENANCE.json)',
                              'official_enry_module':baseline_module,'official_enry_probe_sha256':hashlib.sha256(baseline.read_bytes()).hexdigest(),
                              'probe_sha256':hashlib.sha256(probe.read_bytes()).hexdigest(),'generator_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest()},
                'summary':summary,'results':results}
        args.output.parent.mkdir(parents=True,exist_ok=True)
        args.output.write_text(json.dumps(report,indent=2)+'\n')
        md=['# Upstream sample classifier differential','','This is exploratory classification evidence, not a claim of repository or universal language parity.','',
            f"Pinned Linguist9.7.0 samples: {summary['samples']} files in {summary['sample_language_directories']} sample directories.",'',
            '| Classifier | Exact Ruby label matches | Mismatches |','|---|---:|---:|',
            f"| Enry | {summary['enry_matches']} | {len(results)-summary['enry_matches']} |",f"| Dircue maintained Enry classifier | {summary['auragaze_matches']} | {len(results)-summary['auragaze_matches']} |",'', f"Exact ordered token sequences: {summary['token_sequences_match']}/{len(results)} (SHA-256 of NUL-separated tokens, plus token count).",'',
            '## Dircue differences','','| Sample | Ruby | Ruby type | Enry | Dircue |','|---|---|---|---|---|']
        for x in results:
            if not x['auragaze_matches']:
                escape=lambda value:str(value).replace('|','\\|').replace('\n','\\n')
                md.append('| '+' | '.join(escape(v) for v in [x['path'],x['reference'].get('language',x['reference'].get('error')),x['reference'].get('language_type',''),x['enry'],x['auragaze']])+' |')
        args.output.with_suffix('.md').write_text('\n'.join(md)+'\n')
        print(json.dumps(summary))
        mismatched = summary['auragaze_matches'] != len(results) or summary['token_sequences_match'] != len(results)
        return 1 if summary['reference_errors'] or (args.require_match and mismatched) else 0


if __name__=='__main__':
    raise SystemExit(main())
