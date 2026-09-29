#!/usr/bin/env python3
"""Build a deterministic, firmware-specific RutOS WebUI upload package.

The public upload contains only main + one IPK. RutOS 7.25 uses Yocto
architecture names and the standard ar IPK container. Older RutOS uses its
legacy architecture name and gzip/tar IPK container. Hashes remain embedded
in the required package metadata, not as separate release artifacts.
"""
from __future__ import annotations
import argparse
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]
PACKAGE = 'tlt_custom_pkg_yachtsense-link-emulator'
LEGACY_ARCH = 'arm_cortex-a7_neon-vfpv4'
YOCTO_ARCH = 'cortexa7hf-neon-vfpv4'
EPOCH = 1577836800

def archive(files: dict[str, tuple[bytes, int]]) -> bytes:
    output = io.BytesIO()
    with gzip.GzipFile(fileobj=output, filename='', mode='wb', mtime=0, compresslevel=9) as compressed:
        with tarfile.open(fileobj=compressed, mode='w', format=tarfile.GNU_FORMAT) as tar:
            directories = set()
            for name in files:
                for parent in Path(name).parents:
                    if str(parent) != '.': directories.add(parent.as_posix())
            for name in sorted(directories):
                item=tarfile.TarInfo('./'+name+'/');item.type=tarfile.DIRTYPE;item.mode=0o755;item.mtime=EPOCH
                item.uid=item.gid=0;item.uname=item.gname='root';tar.addfile(item)
            for name, (data, mode) in sorted(files.items()):
                item=tarfile.TarInfo('./'+name);item.size=len(data);item.mode=mode;item.mtime=EPOCH
                item.uid=item.gid=0;item.uname=item.gname='root';tar.addfile(item,io.BytesIO(data))
    return output.getvalue()

def target_profile(firmware: str) -> tuple[str, str]:
    """Select the packaging ABI from the firmware, never only its file label."""
    match = re.fullmatch(r'RUTX_R_00\.07\.(\d+)\.(\d+)', firmware)
    if not match:
        raise ValueError('Expected an exact RUTX firmware, e.g. RUTX_R_00.07.25.3')
    if int(match.group(1)) >= 25:
        return YOCTO_ARCH, 'ar'
    return LEGACY_ARCH, 'tar'


def ar_archive(members: list[tuple[str, bytes]]) -> bytes:
    """Write a deterministic System V ar archive, the opkg-utils default."""
    result = bytearray(b'!<arch>\n')
    for name, payload in members:
        if len(name) > 15 or not re.fullmatch(r'[A-Za-z0-9_.-]+', name):
            raise ValueError('Only short, safe ar member names are supported')
        header = (f'{name + "/":<16}{0:<12}{0:<6}{0:<6}'
                  f'{"100644":<8}{len(payload):<10}`\n').encode('ascii')
        if len(header) != 60:
            raise ValueError('Invalid ar header length')
        result.extend(header)
        result.extend(payload)
        if len(payload) % 2:
            result.extend(b'\n')
    return bytes(result)

def build(firmware: str) -> Path:
    arch, ipk_format = target_profile(firmware)
    version=(ROOT/'VERSION').read_text().strip()
    if not re.fullmatch(r'\d+\.\d+\.\d+',version):raise ValueError('Invalid package version')
    work=ROOT/'build';dist=ROOT/'dist';work.mkdir(exist_ok=True);dist.mkdir(exist_ok=True)
    binary=work/'yachtsense-link-emulator-armv7'
    env={**os.environ,'CGO_ENABLED':'0','GOOS':'linux','GOARCH':'arm','GOARM':'7','GOTOOLCHAIN':'local'}
    subprocess.run(['go','build','-trimpath','-buildvcs=false','-ldflags',f'-s -w -buildid= -X main.packageVersion={version}','-o',str(binary),'./cmd/yachtsense-link-emulator'],cwd=ROOT,env=env,check=True,timeout=240)
    files={}
    for path in sorted((ROOT/'package/root').rglob('*')):
        if path.is_file():
            name=path.relative_to(ROOT/'package/root').as_posix()
            mode=0o755 if name.startswith('etc/init.d/') else 0o644
            files[name]=(path.read_bytes(),mode)
    files['usr/sbin/yachtsense-link-emulator']=(binary.read_bytes(),0o755)
    files['usr/share/doc/yachtsense-link-emulator/LICENSE']=((ROOT/'LICENSE').read_bytes(),0o644)
    data=archive(files)
    # Installed-Size is a conservative byte count of the uncompressed payload.
    # CGO is disabled: do not introduce an unnecessary distro-specific libc dependency.
    size=sum(len(content) for content,_ in files.values())
    control=f'''Package: {PACKAGE}
Version: {version}-1
Architecture: {arch}
Router: RUTX
tlt_name: yachtsense-link-emulator
Maintainer: Roel Broersma
Section: net
Priority: optional
Depends: api-core, vuci-ui-core
Installed-Size: {size}
Description: YachtSense Link discovery and HTTP health with selective Raymarine mDNS relay, observed runtime status, persistent network choices and bounded native service actions.
'''.encode()
    postinst=b'''#!/bin/sh
[ "${IPKG_NO_SCRIPT:-}" = "1" ] && exit 0
[ -s "${IPKG_INSTROOT:-}/lib/functions.sh" ] || exit 0
. "${IPKG_INSTROOT:-}/lib/functions.sh"
default_postinst "$0" "$@"
'''
    prerm=b'''#!/bin/sh
[ -s "${IPKG_INSTROOT:-}/lib/functions.sh" ] || exit 0
. "${IPKG_INSTROOT:-}/lib/functions.sh"
default_prerm "$0" "$@"
'''
    controls={'control':(control,0o644),'conffiles':(b'/etc/config/yachtsense_link_emulator\n',0o644),'postinst':(postinst,0o755),'prerm':(prerm,0o755)}
    for name in ['postinst-pkg','prerm-pkg','postrm']:
        controls[name]=((ROOT/'package/control'/name).read_bytes(),0o755)
    ctrl=archive(controls)
    ipk_name=f'{PACKAGE}_{version}-1_{arch}.ipk'
    # Use the standard Yocto ar container while retaining legacy firmware support.
    if ipk_format == 'ar':
        ipk=ar_archive([('debian-binary', b'2.0\n'), ('control.tar.gz', ctrl), ('data.tar.gz', data)])
    else:
        ipk=archive({'debian-binary':(b'2.0\n',0o644),'data.tar.gz':(data,0o644),'control.tar.gz':(ctrl,0o644)})
    checksum=hashlib.sha256(data+ctrl).hexdigest()
    main=f'''Package: {PACKAGE}
Version: {version}-1
Architecture: {arch}
Router: RUTX
Firmware: {firmware}
tlt_name: yachtsense-link-emulator
ipk_file: {ipk_name}:{checksum}
ipk_deps:
'''.encode()
    final=dist/f'yachtsense-link-emulator_{version}-1_RUTX_{firmware.rsplit("_",1)[1]}.tar.gz'
    final.write_bytes(archive({'main':(main,0o644),ipk_name:(ipk,0o644)}))
    # The standalone IPK stays in build/ for auditing; it is not a release attachment.
    (work/ipk_name).write_bytes(ipk)
    print(f'Built {final} ({final.stat().st_size} bytes)')
    return final

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--firmware',default='RUTX_R_00.07.25.3')
    build(parser.parse_args().firmware)
