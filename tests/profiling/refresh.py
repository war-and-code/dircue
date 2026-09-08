#!/usr/bin/env python3
"""Prepare and run a committed scanner refresh. Each phase requires explicit invocation."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import stat
import subprocess
import tempfile
import time

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[1]
IMAGE='sha256:f67c429e2b22c0c9691c9e16cceeeee9f2033486b451f5dc0f4278a1783f742c'
# Existing frozen corpus volumes keep their original local resource names.
GIT_VOLUME='auragaze-v01-corpus-20260907'
FLAT_VOLUME='auragaze-enry-corpus-flat-rc1'


def sha(path):
    h=hashlib.sha256()
    with path.open('rb') as f:
        for b in iter(lambda:f.read(1024*1024),b''):h.update(b)
    return h.hexdigest()


def write(path,value):
    with path.open('x') as f:json.dump(value,f,indent=2,sort_keys=True);f.write('\n')


def command(argv,**kw):
    return subprocess.run(argv,check=True,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,**kw).stdout.strip()


def source_identity(commit):
    assert command(['git','rev-parse','HEAD'],cwd=ROOT)==commit,'HEAD differs from requested commit'
    assert not command(['git','status','--porcelain','--untracked-files=all'],cwd=ROOT),'source must be committed and clean'
    files=command(['git','ls-files','-z'],cwd=ROOT).split('\0')
    selected=[n for n in files if n in ('go.mod','go.sum','main.go') or n.startswith(('internal/','pkg/','third_party/go-enry/'))]
    assert selected and 'pkg/scanner/profiling_benchmark_test.go' in selected
    return {n:sha(ROOT/n) for n in selected}


def docker(args,phase):
    return ['docker','run','--rm','--platform','linux/arm64','--network','none','--read-only',
        '--cap-drop','ALL','--security-opt','no-new-privileges','--user','65532:65532',
        '-v',f'{GIT_VOLUME}:/corpus:ro','-v',f'{FLAT_VOLUME}:/flat:ro',
        '-v',f'{ROOT}/tests:/harness:ro','-v',f'{args.output}:/build:rw',
        '-e','GODEBUG=','-e','GOGC=100','-e','GOMEMLIMIT=off','-e','GOMAXPROCS=5',
        '-e','GIT_CONFIG_NOSYSTEM=1','-e','GIT_CONFIG_GLOBAL=/dev/null','-e','GIT_NO_REPLACE_OBJECTS=1',
        IMAGE,'python3','/harness/profiling/refresh.py','--phase',phase,'--output','/build','--commit',args.commit]


def prepare(args):
    assert not args.output.exists(),'output must not exist'
    before=source_identity(args.commit)
    args.output.mkdir(parents=True)
    old=ROOT/'.cache/scanner-profiles-rc1'
    flat=ROOT/'.cache/enry-corpus-flat-rc1'
    for name in ['fixture-roslyn.json']:
        shutil.copyfile(old/name,args.output/name)
    shutil.copyfile(flat/'summary.json',args.output/'flat-summary.json')
    summary=json.loads((flat/'summary.json').read_text())
    assert sha(flat/'provenance.json')==summary['provenance_sha256']
    manifest=json.loads((flat/'provenance.json').read_text())
    selected={p['name']:p for p in manifest['projects'] if p['name'] in ('jq','ripgrep')}
    write(args.output/'selected-flat-files.json',selected)
    for name in ['scanner-small-baseline','scanner-dotnet-baseline']:
        original=ROOT/'.cache/enry-results-rc1'/name/'result.json'
        data=json.loads(original.read_text())
        assert data['complete'] and data['identity']['binary_sha256']=='a9dce8fb9e23899086a140540069fffc283d65ea3d7ca4ef49be4b0b946e1a02'
        assert sha(ROOT/'tests/profiling/run.py')==data['identity']['runner_sha256']
        assert sha(args.output/'fixture-roslyn.json')==data['identity']['receipts']['git']['sha256']
        assert sha(args.output/'flat-summary.json')==data['identity']['receipts']['flat']['sha256']
        shutil.copyfile(original,args.output/(name+'-rc1.json'))
    write(args.output/'source-before-build.json',{'commit':args.commit,'files':before})
    env={k:v for k,v in os.environ.items() if not k.startswith(('GO','CGO'))}
    controls={'CGO_ENABLED':'0','GOOS':'linux','GOARCH':'arm64','GOARM64':'v8.0',
        'GOTOOLCHAIN':'go1.26.6','GOENV':'off','GOWORK':'off','GOFLAGS':'','GOEXPERIMENT':'',
        'GOPROXY':'https://proxy.golang.org,direct','GOSUMDB':'sum.golang.org','GOAUTH':'off',
        'GOPRIVATE':'','GONOPROXY':'','GONOSUMDB':'','GOINSECURE':'',
        'GOGC':'100','GOMEMLIMIT':'off','GODEBUG':''}
    env.update(controls)
    with tempfile.TemporaryDirectory(prefix='dircue-scanner-build-') as temp:
        env.update(GOCACHE=temp+'/build-cache',GOMODCACHE=temp+'/module-cache')
        version=command([args.go,'version'],env=env,cwd=ROOT)
        assert version.startswith('go version go1.26.6 '),version
        build=[args.go,'test','-c','-trimpath','-buildvcs=false','-mod=readonly','-o',str(args.output/'scanner.test'),'./pkg/scanner']
        with (args.output/'build.stdout').open('x') as out,(args.output/'build.stderr').open('x') as err:
            subprocess.run(build,env=env,cwd=ROOT,stdout=out,stderr=err,check=True)
        modules=command([args.go,'version','-m',str(args.output/'scanner.test')],env=env,cwd=ROOT)
        assert 'go1.26.6' in modules and 'CGO_ENABLED=0' in modules and 'GOARM64=v8.0' in modules
    after=source_identity(args.commit)
    assert before==after,'source changed during build'
    receipt={'complete':True,'purpose':'optimized unstripped first-scan measurement; before getLines optimization',
        'commit':args.commit,'source_files_sha256':before,'source_unchanged_after_build':True,
        'build_command':build,'environment':controls,'go_version':version,'binary_modules':modules,
        'binary_sha256':sha(args.output/'scanner.test'),'binary_bytes':(args.output/'scanner.test').stat().st_size,
        'file_description':command(['file',str(args.output/'scanner.test')]),
        'fresh_caches':True,'profile_runner_sha256':sha(HERE/'run.py'),'refresh_script_sha256':sha(Path(__file__)),
        'build_stdout_sha256':sha(args.output/'build.stdout'),'build_stderr_sha256':sha(args.output/'build.stderr')}
    assert 'not stripped' in receipt['file_description'] and 'statically linked' in receipt['file_description']
    write(args.output/'build-receipt.json',receipt)
    write(args.output/'host-launcher.json',{'created_at_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),
        'host_platform':platform.platform(),'host_cpu_count':os.cpu_count(),
        'docker_version':json.loads(command(['docker','version','--format','{{json .}}'])),
        'image':json.loads(command(['docker','image','inspect',IMAGE])),
        'limits':'Docker Desktop VM; uncontrolled governor/turbo/SMT and OS caches; no independent quiet-host trace'})
    cmd=docker(args,'preflight')
    write(args.output/'preflight-command.json',cmd)
    print(command(cmd),flush=True)
    write(args.output/'ready.json',{'complete':True,'commit':args.commit,'binary_sha256':receipt['binary_sha256'],
        'receipts':{n:sha(args.output/n) for n in ['build-receipt.json','host-receipt.json','fixture-roslyn.json',
        'flat-summary.json','fixture-verification.json','scanner-small-baseline-rc1.json','scanner-dotnet-baseline-rc1.json']}})
    print('PREPARED. Start run separately after coordination confirms a quiet host.',flush=True)


def preflight(args):
    assert platform.system()=='Linux'
    selected=json.loads((args.output/'selected-flat-files.json').read_text())
    counts={}
    for name,project in selected.items():
        root=Path('/flat')/name
        actual={p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_file() or p.is_symlink()}
        assert actual=={r['path'] for r in project['files']},'flat file population changed'
        for row in project['files']:
            path=root/row['path'];info=path.lstat()
            assert stat.S_ISREG(info.st_mode) and info.st_size==row['bytes']
            assert sha(path)==row['sha256']
            assert bool(info.st_mode&0o111)==(row['mode']=='100755')
        counts[name]={'files':len(actual),'bytes':sum(r['bytes'] for r in project['files']),
            'commit':project['commit'],'tree':project['tree'],'verification':'full byte hash, size, path population and executable mode readback'}
    roslyn=json.loads((args.output/'fixture-roslyn.json').read_text())
    git=['git','--no-replace-objects','-c','core.hooksPath=/dev/null','-c','safe.directory=*','-C','/corpus/roslyn']
    assert command(git+['rev-parse','HEAD'])==roslyn['observed_head']
    assert command(git+['rev-parse','HEAD^{tree}'])==roslyn['observed_tree']
    # Full fsck verifies packed/loose object hashes without executing checkout code.
    fsck=command(git+['fsck','--full','--no-reflogs','--no-dangling'])
    counts['roslyn']={'commit':roslyn['observed_head'],'tree':roslyn['observed_tree'],
        'storage':command(git+['count-objects','-v']),'fsck_stdout':fsck,
        'verification':'pinned HEAD/tree and full object integrity check; baseline result must also match RC1'}
    write(args.output/'fixture-verification.json',{'complete':True,'selected':counts,'source_volumes_read_only':True})
    probes={}
    for name in ['/proc/meminfo','/proc/self/cgroup','/sys/fs/cgroup/cpu.max','/sys/fs/cgroup/memory.max','/sys/fs/cgroup/cpu.stat']:
        path=Path(name);probes[name]=path.read_text() if path.exists() else None
    write(args.output/'host-receipt.json',{'created_at_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),
        'platform':platform.platform(),'cpu_count':os.cpu_count(),'affinity':sorted(os.sched_getaffinity(0)),
        'load_average':os.getloadavg(),'probes':probes,'image':IMAGE,
        'host_launcher_sha256':sha(args.output/'host-launcher.json'),
        'cache_policy':'uncontrolled and warmed by integrity verification; first-scan is fresh process state only',
        'runtime':{'GOGC':'100','GOMEMLIMIT':'off','GODEBUG':'','GOMAXPROCS':'5'}})
    print('Selected immutable fixtures verified; host receipt refreshed.')


def sequence(args):
    assert platform.system()=='Linux'
    ready=json.loads((args.output/'ready.json').read_text())
    assert ready['commit']==args.commit and sha(args.output/'scanner.test')==ready['binary_sha256']
    for n,h in ready['receipts'].items():assert sha(args.output/n)==h,n
    receipt=json.loads((args.output/'build-receipt.json').read_text())
    assert sha(HERE/'run.py')==receipt['profile_runner_sha256']
    assert sha(Path(__file__))==receipt['refresh_script_sha256']
    jobs=[('small-baseline','baseline',100,['ripgrep-directory-w1','jq-directory-w1'],None),
        ('dotnet-baseline','baseline',20,['roslyn-git-w5'],None),
        ('small-cpu','cpu',100,['ripgrep-directory-w1','jq-directory-w1'],'small-baseline'),
        ('dotnet-mem','mem',20,['roslyn-git-w5'],'dotnet-baseline')]
    results={}
    for name,stage,runs,scenarios,baseline in jobs:
        output=args.output/('scanner-'+name)
        cmd=['python3',str(HERE/'run.py'),'--binary',str(args.output/'scanner.test'),'--output',str(output),
            '--git-root','/corpus','--flat-root','/flat','--git-receipt',str(args.output/'fixture-roslyn.json'),
            '--flat-receipt',str(args.output/'flat-summary.json'),'--build-receipt',str(args.output/'build-receipt.json'),
            '--host-receipt',str(args.output/'host-receipt.json'),'--stage',stage,'--runs',str(runs),'--warmups','3',
            '--gomaxprocs','5','--include-files','0']
        for scenario in scenarios:cmd.extend(['--scenario',scenario])
        if baseline:cmd.extend(['--baseline',str(args.output/('scanner-'+baseline)/'result.json')])
        subprocess.run(cmd,check=True)
        result=json.loads((output/'result.json').read_text())
        assert result['complete']
        historical=args.output/('scanner-'+('small' if name.startswith('small') else 'dotnet')+'-baseline-rc1.json')
        previous=json.loads(historical.read_text())
        assert result['result_digests']==previous['result_digests'],'result digest differs from RC1'
        results[name]={'report_sha256':sha(output/'result.json'),'digests_identical_to_rc1':True,
            'rc1_report_sha256':sha(historical),'summaries':result.get('summaries'),
            'interpretation':'separate historical window; profiled samples never included in baseline metrics'}
    write(args.output/'sequence-receipt.json',{'complete':True,'commit':args.commit,'results':results})


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--phase',required=True,choices=['prepare','run','preflight','sequence'])
    p.add_argument('--commit',required=True,help='exact committed opt2 HEAD; never inferred')
    p.add_argument('--output',type=Path,default=ROOT/'.cache/scanner-profiles-opt2')
    p.add_argument('--go',default='go')
    a=p.parse_args();a.output=a.output.resolve()
    if a.phase=='prepare':prepare(a)
    elif a.phase=='preflight':preflight(a)
    elif a.phase=='sequence':sequence(a)
    else:
        ready=json.loads((a.output/'ready.json').read_text())
        assert ready['commit']==a.commit and source_identity(a.commit)==json.loads((a.output/'build-receipt.json').read_text())['source_files_sha256']
        assert not (a.output/'sequence-command.json').exists(),'run requires fresh sequence output'
        cmd=docker(a,'sequence');write(a.output/'sequence-command.json',cmd)
        with (a.output/'sequence.stdout').open('x') as out,(a.output/'sequence.stderr').open('x') as err:
            subprocess.run(cmd,stdout=out,stderr=err,check=True)
        assert source_identity(a.commit)==json.loads((a.output/'build-receipt.json').read_text())['source_files_sha256']
        print('COMPLETE: all scanner baseline/profile digests match RC1.')

if __name__=='__main__':main()
