import hashlib,json,os,pathlib,subprocess,sys,tempfile
root=pathlib.Path.cwd()
wheel=next((root/'dist/dircue-wheels-rc3').glob('*macosx_12_0_arm64.whl'))
direct=root/'dist/dircue-rc3/bin/dircue-darwin-arm64'
checks=[]
with tempfile.TemporaryDirectory(prefix='dircue wheel smoke ') as temp:
 t=pathlib.Path(temp);fixture=t/'source with spaces';fixture.mkdir()
 (fixture/'Main.cs').write_text('using System; class Hello { static void Main() {} }\n')
 (fixture/'App.csproj').write_text('<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n')
 (fixture/'Hello.java').write_text('class Hello { public static void main(String[] args) {} }\n')
 (fixture/'data.xml').write_text('<events><event>log</event></events>\n')
 env={k:v for k,v in os.environ.items() if not k.startswith('UV_')}
 env.update(UV_CACHE_DIR=str(t/'cache'),UV_TOOL_DIR=str(t/'tools'),UV_TOOL_BIN_DIR=str(t/'toolbin'),UV_PYTHON_DOWNLOADS='never',UV_NO_CONFIG='true')
 def run(args,expected=0):
  p=subprocess.run([str(x) for x in args],cwd=fixture,env=env,text=True,capture_output=True,timeout=120)
  if p.returncode!=expected:raise RuntimeError((args,p.returncode,p.stdout,p.stderr))
  checks.append({'command':[str(x).replace(str(t),'<temporary>').replace(str(root),'<repository>') for x in args],'exit_code':p.returncode,'stdout':p.stdout.replace(str(t),'<temporary>'),'stderr':p.stderr.replace(str(t),'<temporary>')})
  return p
 base=['uvx','--offline','--no-index','--no-python-downloads','--python',sys.executable,'--from',wheel,'dircue']
 assert run(base+['--version']).stdout=='dircue 0.1.0\n'
 for args in [['--breakdown','--json','.'],['analyze','all','--json','.']]:
  assert run(base+args).stdout==run([direct,*args]).stdout
 err=run(base+['--json','./missing'],1);actual=run([direct,'--json','./missing'],1)
 assert err.stdout==actual.stdout=='' and err.stderr.endswith(actual.stderr)
 run(['uv','tool','install','--offline','--no-index','--python',sys.executable,str(wheel)])
 installed=t/'toolbin/dircue';assert run([installed,'--version']).stdout=='dircue 0.1.0\n'
 assert run([installed,'analyze','all','--json','.']).stdout==run([direct,'analyze','all','--json','.']).stdout
 # A conventional pip installation in a disposable environment, without network.
 venv=t/'pip-env';run([sys.executable,'-m','venv',venv])
 py=venv/'bin/python';run([py,'-m','pip','install','--no-index','--disable-pip-version-check',wheel])
 assert run([py,'-m','dircue','--version']).stdout=='dircue 0.1.0\n'
 assert run([venv/'bin/dircue','--breakdown','--json','.']).stdout==run([direct,'--breakdown','--json','.']).stdout
 code='import hashlib,dircue;print(hashlib.sha256(open(dircue.get_binary_path(),"rb").read()).hexdigest())'
 assert run([py,'-c',code]).stdout.strip()==hashlib.sha256(direct.read_bytes()).hexdigest()
result={'complete':True,'platform':'native macOS arm64','python':sys.version,'wheel':wheel.name,'wheel_sha256':hashlib.sha256(wheel.read_bytes()).hexdigest(),'binary_sha256':hashlib.sha256(direct.read_bytes()).hexdigest(),'checks':checks}
(root/'.cache/pypi-preparation/native-smoke.json').write_text(json.dumps(result,indent=2)+'\n')
print('PASS: uvx, uv tool install, pip, python -m dircue, exact JSON/error output and binary identity; offline local wheel.')
