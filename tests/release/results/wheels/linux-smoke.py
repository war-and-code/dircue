import hashlib,json,pathlib,subprocess,tempfile
root=pathlib.Path.cwd();wheelroot=root/'dist/dircue-wheels-rc3';checks=[]
images=[('glibc','ghcr.io/astral-sh/uv:python3.13-bookworm-slim','manylinux_2_17_aarch64'),('musl','ghcr.io/astral-sh/uv:python3.13-alpine','musllinux_1_2_aarch64')]
with tempfile.TemporaryDirectory(prefix='dircue-linux-wheel-') as temp:
 fixture=pathlib.Path(temp)/'fixture';fixture.mkdir(mode=0o755)
 (fixture/'Main.cs').write_text('using System; class Hello { static void Main() {} }\n')
 (fixture/'Hello.java').write_text('class Hello { public static void main(String[] args) {} }\n')
 expected=json.loads(subprocess.check_output([str(root/'dist/dircue-rc3/bin/dircue-darwin-arm64'),'-bj','.'],cwd=fixture))
 for libc,image,tag in images:
  imageinfo=json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]
  assert imageinfo['Architecture']=='arm64'
  wheel=next(wheelroot.glob('*'+tag+'.whl'))
  common=['docker','run','--rm','--network','none','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','--user','65532:65532','--tmpfs','/tmp:rw,exec,mode=1777,size=512m','-e','HOME=/tmp','-e','UV_CACHE_DIR=/tmp/cache','-e','UV_PYTHON_DOWNLOADS=never','-v',str(wheelroot)+':/wheels:ro','-v',str(fixture)+':/repo:ro','-w','/repo','--entrypoint','uvx',imageinfo['Id'],'--offline','--no-index','--no-config','--from','/wheels/'+wheel.name,'dircue']
  for args,status in [(['--version'],0),(['-bj','.'],0),(['analyze','all','--json','.'],0),(['--json','./missing'],1)]:
   p=subprocess.run(common+args,text=True,capture_output=True,timeout=120)
   if p.returncode!=status:raise RuntimeError((libc,args,p.returncode,p.stdout,p.stderr))
   if args==['--version']:assert p.stdout=='dircue 0.1.0\n'
   elif args==['-bj','.']:assert json.loads(p.stdout)==expected
   elif args[0]=='analyze':assert {l['name'] for l in json.loads(p.stdout)['languages']}=={'Java','C#'}
   else:assert not p.stdout and 'missing' in p.stderr
   checks.append({'libc':libc,'image_id':imageinfo['Id'],'wheel':wheel.name,'wheel_sha256':hashlib.sha256(wheel.read_bytes()).hexdigest(),'args':args,'exit_code':p.returncode,'stdout':p.stdout,'stderr':p.stderr})
(root/'.cache/pypi-preparation/linux-smoke.json').write_text(json.dumps({'complete':True,'platform':'Linux arm64 Docker Desktop','network':'none','uid':65532,'checks':checks},indent=2)+'\n')
print('PASS: Linux glibc and musl wheels through uvx offline, unprivileged, Java/C# JSON, errors.')
