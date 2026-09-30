"""Inspect the actual release archives and the complete RutOS permission chain."""
from __future__ import annotations
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import struct
import subprocess
import tarfile
import tempfile

ROOT=Path(__file__).resolve().parents[1]
VERSION=(ROOT/'VERSION').read_text().strip()
FINAL=ROOT/f'dist/yachtsense-link-emulator_{VERSION}-1_RUTX_00.07.25.3.tar.gz'
METHODS={'status','diagnostics','save','start','stop','restart'}
checks=0

def check(name, value):
    global checks
    assert value, name
    checks+=1
    print('PASS '+name)

def unpack(blob):
    result={}
    with tarfile.open(fileobj=io.BytesIO(blob),mode='r:gz') as t:
        for m in t.getmembers():
            name=m.name.removeprefix('./')
            assert not name.startswith('/') and '..' not in Path(name).parts
            assert m.uid==m.gid==0 and not m.issym() and not m.islnk()
            if m.isfile(): result[name]=(t.extractfile(m).read(),m.mode)
    return result

def fields(blob):
    return {k:v.strip() for k,v in (line.split(':',1) for line in blob.decode().splitlines() if line.strip())}

outer=unpack(FINAL.read_bytes())
main=fields(outer['main'][0]);ipk_name,digest=main['ipk_file'].split(':');ipk_raw=outer[ipk_name][0]
check('WebUI wrapper includes the custom feed index',set(outer)=={'main',ipk_name,'Packages','Packages.gz'})
check('Compressed feed equals plain feed',gzip.decompress(outer['Packages.gz'][0])==outer['Packages'][0])
index=fields(outer['Packages'][0])
check('Feed Filename resolves to the included IPK',index['Filename']==ipk_name)
check('Feed Size matches compressed IPK',int(index['Size'])==len(ipk_raw))
check('Feed hashes match complete IPK',index['SHA256sum']==hashlib.sha256(ipk_raw).hexdigest() and index['MD5Sum']==hashlib.md5(ipk_raw).hexdigest())
check('Exact firmware target',main['Firmware']=='RUTX_R_00.07.25.3')
check('Native Yocto architecture',main['Architecture']=='cortexa7hf-neon-vfpv4')
check('Standard ar container',ipk_raw.startswith(b'!<arch>\n'))
with tempfile.TemporaryDirectory() as d:
    path=Path(d)/'pkg.ipk';path.write_bytes(ipk_raw)
    members=subprocess.check_output(['ar','t',str(path)],text=True).splitlines()
    check('Independent ar reader sees three standard members',members==['debian-binary','control.tar.gz','data.tar.gz'])
    contents={n:subprocess.check_output(['ar','p',str(path),n]) for n in members}
check('Debian format marker',contents['debian-binary']==b'2.0\n')
check('RutOS payload/control integrity hash',digest==hashlib.sha256(contents['data.tar.gz']+contents['control.tar.gz']).hexdigest())
control=unpack(contents['control.tar.gz']);data=unpack(contents['data.tar.gz']);metadata=fields(control['control'][0])
check('Identity consistent across all metadata',all(main[k]==metadata[k]==index[k] for k in ['Package','Version','Architecture','Router']))
check('Release version',metadata['Version']==VERSION+'-1')
check('IPK name matches identity',ipk_name==f'{metadata["Package"]}_{metadata["Version"]}_{metadata["Architecture"]}.ipk')
check('Static backend has no libc dependency',metadata['Depends']=='api-core, vuci-ui-core')
check('Installed size matches the actual payload',int(metadata['Installed-Size'])==sum(len(v[0]) for v in data.values()))
check('Configuration is preserved on upgrade',control['conffiles'][0]==b'/etc/config/yachtsense_link_emulator\n')
for name in ['usr/sbin/yachtsense-link-emulator','etc/init.d/yachtsense-link-emulator','usr/libexec/rpcd/yachtsense-link-emulator','usr/libexec/yachtsense-link-emulator-reload-ui']:
    check('Executable mode: '+name,data[name][1]==0o755)
rpc=json.loads(data['usr/share/acl.d/rpcd_yachtsense_link_emulator.json'][0])
check('rpcd can publish only this object',rpc['user']=='rpcd' and rpc['publish']==['yachtsense-link-emulator'])
check('rpcd can discover and invoke its own six methods',set(rpc['access'])=={'yachtsense-link-emulator'} and set(rpc['access']['yachtsense-link-emulator']['methods'])==METHODS)
web=json.loads(data['usr/share/acl.d/yachtsense-link-emulator.json'][0])
check('Web user has only finite helper access',web['user']=='uhttpd' and set(web['access'])=={'yachtsense-link-emulator'} and set(web['access']['yachtsense-link-emulator']['methods'])==METHODS)
session=json.loads(data['usr/share/rpcd/acl.d/yachtsense-link-emulator.json'][0])['services/yachtsense-link-emulator']
check('Session read access excludes mutations',set(session['read']['ubus']['yachtsense-link-emulator'])=={'status','diagnostics'})
check('Session write access is separate',set(session['write']['ubus']['yachtsense-link-emulator'])==METHODS-{'status','diagnostics'})
check('API ACL supports normalized trailing slash','/yachtsense-link-emulator-v1100/status/' in session['read']['api'])
menu=json.loads(data['usr/share/vuci/menu.d/yachtsense-link-emulator.json'][0]);view=menu['services/yachtsense-link-emulator']['view']
check('Versioned menu view is shipped','www/views/'+view+'.js' in data and view.endswith('V1220'))
check('New stylesheet URL exists','www/assets/yachtsense-link-emulator-v1220.css' in data)
routes=json.loads(data['usr/share/vuci/path.d/yachtsense-link-emulator.json'][0])
check('Each route has a shipped adapter',all('usr/lib/lua/api/'+module+'.lua' in data for item in routes for module in item.values()))
reload=data['usr/libexec/yachtsense-link-emulator-reload-ui'][0].decode()
check('Bus ACL reload precedes RPC registration',reload.index('killall -HUP ubusd')<reload.index('/etc/init.d/rpcd restart')<reload.index('killall -HUP uhttpd'))
check('UI registration does not restart networking','/etc/init.d/network' not in reload and '/etc/init.d/yachtsense-link-emulator' not in reload)
check('Postinstall refresh is detached from upload request',b'</dev/null >/dev/null 2>&1 &' in control['postinst'][0])
check('Native lifecycle delegation retained',b'default_postinst "$0" "$@"' in control['postinst'][0] and b'default_prerm "$0" "$@"' in control['prerm'][0])
check('Removal distinguishes an upgrade',b'PKG_UPGRADE' in control['postrm'][0] and b"'upgrade'" in control['postrm'][0])
check('Observe-only network defaults',b"option manage_ip '0'" in data['etc/config/yachtsense_link_emulator'][0] and b"option remove_ip_on_stop '0'" in data['etc/config/yachtsense_link_emulator'][0])
elf=data['usr/sbin/yachtsense-link-emulator'][0]
check('Little-endian ELF32 ARM binary',elf[:6]==b'\x7fELF\x01\x01' and struct.unpack_from('<H',elf,18)[0]==40)
phoff=struct.unpack_from('<I',elf,28)[0];phentsz,phnum=struct.unpack_from('<HH',elf,42)
check('Static binary without ELF interpreter',3 not in [struct.unpack_from('<I',elf,phoff+i*phentsz)[0] for i in range(phnum)])
with tempfile.TemporaryDirectory() as d:
    d=Path(d)
    for name in ['postinst','prerm','postinst-pkg','prerm-pkg','postrm']:
        path=d/name;path.write_bytes(control[name][0]);subprocess.run(['sh','-n',str(path)],check=True)
    for name in ['etc/init.d/yachtsense-link-emulator','usr/libexec/yachtsense-link-emulator-reload-ui']:
        path=d/'script';path.write_bytes(data[name][0]);subprocess.run(['sh','-n',str(path)],check=True)
    check('All shell hooks pass syntax validation',True)
    for name in ['postinst','prerm','postrm']:
        subprocess.run(['sh',str(d/name)],env={**os.environ,'IPKG_INSTROOT':str(d/'offline'),'PKG_UPGRADE':'1'},check=True)
    check('Offline hooks do not touch the live system',True)
print(f'{checks} release package checks passed (not a complete RutOS installation simulation)')
