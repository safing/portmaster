#!/usr/bin/env python3
"""Render Tauri's NSIS installer template on Linux and build the Windows installer with makensis.

The tauri-cli refuses `--bundles nsis` on non-Windows hosts although tauri-bundler itself
supports it. This script reproduces the bundler's data map for Portmaster and runs makensis.

Prerequisites (Debian/Ubuntu): apt install nsis mingw-w64 lld llvm
  - tauri-bundler sources (for installer.nsi/utils.nsh/languages): pass --bundler-src
  - nsis_tauri_utils.dll in ~/.cache/tauri/NSIS/Plugins/x86-unicode (downloaded automatically)
  - desktop/tauri/src-tauri/binary/  : portmaster-core.exe, portmaster-kext.sys, portmaster-core.dll,
                                       WebView2Loader.dll, portmaster.zip, assets.zip
  - desktop/tauri/src-tauri/intel/   : intel data (updatemgr download .../intel.v3.json)
  - target/x86_64-pc-windows-gnu/release/portmaster.exe (cargo tauri build --target x86_64-pc-windows-gnu --no-bundle)

Usage: python3 packaging/windows/render_nsis_linux.py <version> [--bundler-src DIR] [--out FILE]
"""
import re, os, sys, argparse, subprocess, hashlib, urllib.request, shutil
ap=argparse.ArgumentParser()
ap.add_argument('version')
ap.add_argument('--bundler-src', default=os.path.expanduser('~/.cache/tauri-bundler-src'),
                help='directory containing tauri-bundler-<ver>/ (crate sources, default: auto-download 2.2.4)')
ap.add_argument('--out', default=None, help='output installer path')
args=ap.parse_args()
ROOT=os.path.realpath(os.path.join(os.path.dirname(__file__),'..','..'))
ST=f'{ROOT}/desktop/tauri/src-tauri'
# tauri-bundler sources
if not os.path.isdir(args.bundler_src) or not any(d.startswith('tauri-bundler-') for d in os.listdir(args.bundler_src)):
    os.makedirs(args.bundler_src, exist_ok=True)
    crate=f'{args.bundler_src}/tb.crate'
    urllib.request.urlretrieve('https://crates.io/api/v1/crates/tauri-bundler/2.2.4/download', crate)
    subprocess.check_call(['tar','xzf',crate,'-C',args.bundler_src])
TBROOT=[d for d in os.listdir(args.bundler_src) if d.startswith('tauri-bundler-')][0]
TB=f'{args.bundler_src}/{TBROOT}/src/bundle/windows/nsis'
# plugin
PL=os.path.expanduser('~/.cache/tauri/NSIS/Plugins/x86-unicode'); os.makedirs(PL, exist_ok=True)
dll=f'{PL}/nsis_tauri_utils.dll'
if not os.path.exists(dll):
    urllib.request.urlretrieve('https://github.com/tauri-apps/nsis-tauri-utils/releases/download/nsis_tauri_utils-v0.4.2/nsis_tauri_utils.dll', dll)
if hashlib.sha1(open(dll,'rb').read()).hexdigest().upper()!='6532DA4545864C6EC95F62F27F2199BFD668560B':
    raise SystemExit('nsis_tauri_utils.dll checksum mismatch')
REL=f'{ST}/target/x86_64-pc-windows-gnu/release'
OUT=f'{ST}/target/release/nsis/x64'  # must be exactly 4 levels below src-tauri: install_hooks.nsh uses ..\\..\\..\\.. paths
os.makedirs(OUT, exist_ok=True)
def esc(v):
    if isinstance(v,bool): return 'true' if v else 'false'
    s=str(v); o=''
    for c in s:
        o+={'"':'$\\"','$':'$$','`':'$\\`','\n':'$\\n','\t':'$\\t','\r':'$\\r'}.get(c,c)
    return o
def bom(path, content):
    if isinstance(content,str): content=content.encode()
    open(path,'wb').write(b'\xef\xbb\xbf'+content)
lang_file=f'{OUT}/English.nsh'; bom(lang_file, open(f'{TB}/languages/English.nsh','rb').read())
bom(f'{OUT}/utils.nsh', open(f'{TB}/utils.nsh','rb').read())
bom(f'{OUT}/FileAssociation.nsh', open(f'{TB}/FileAssociation.nsh','rb').read())
main_bin=f'{REL}/portmaster.exe'
size_kb=sum(os.path.getsize(os.path.join(d,f)) for d in [f'{ST}/binary',f'{ST}/intel'] for f in os.listdir(d))//1024 + os.path.getsize(main_bin)//1024
version=args.version
m=re.match(r'(\d+)\.(\d+)\.(\d+)',version)
data=dict(
 compression='lzma', installer_hooks=f'{ST}/templates/nsis/install_hooks.nsh',
 manufacturer='safing', product_name='Portmaster', version=version,
 version_with_build=f'{m.group(1)}.{m.group(2)}.{m.group(3)}.0', homepage='', install_mode='perMachine',
 license='', installer_icon=os.path.realpath(f'{ST}/../../../assets/data/icons/pm_light_contrast.ico'),
 sidebar_image='', header_image='', main_binary_name='portmaster', main_binary_path=main_bin,
 bundle_id='io.safing.portmaster', copyright='Safing Limited Inc', out_file='nsis-output.exe', arch='x64',
 additional_plugins_path=os.path.expanduser('~/.cache/tauri/NSIS/Plugins/x86-unicode'),
 allow_downgrades=True, display_language_selector=False, install_webview2_mode='downloadBootstrapper',
 webview2_installer_args='/silent', webview2_bootstrapper_path='', webview2_installer_path='',
 minimum_webview2_version='', uninstaller_sign_cmd='', estimated_size=size_kb, start_menu_folder='',
 short_description='Portmaster UI', long_description='',
)
lists=dict(languages=['English'], language_files=[lang_file], resources_dirs=[], resources={}, binaries={},
           resources_ancestors=[], file_associations=[], deep_link_protocols=[])
t=open(f'{TB}/installer.nsi').read()
# each blocks (non-nested first: nested ones are file_associations which are empty)
def each_sub(mm):
    name=mm.group(1); body=mm.group(2); items=lists[name]
    out=''
    if isinstance(items,dict):
        for k,v in items.items(): out+=body.replace('{{@key}}',esc(k)).replace('{{this.[1]}}',esc(v[1]) if isinstance(v,(list,tuple)) else esc(v)).replace('{{this}}',esc(v))
    else:
        for v in items: out+=body.replace('{{this}}',esc(v))
    return out
# nested each with "as |x|" -> remove entirely (empty lists); indentation-tolerant
t=re.sub(r'[ \t]*\{\{#each file_associations as \|\w+\| ~\}\}\n.*?\{\{/each\}\}\n[ \t]*\{\{/each\}\}\n','',t,flags=re.S)
t=re.sub(r'[ \t]*\{\{#each deep_link_protocols as \|\w+\| ~\}\}\n.*?\{\{/each\}\}\n','',t,flags=re.S)
assert 'as |' not in t, 'leftover nested each'
t=re.sub(r'[ \t]*\{\{#each (\w+)\}\}\n(.*?)[ \t]*\{\{/each\}\}\n',each_sub,t,flags=re.S)
def if_sub(mm):
    return mm.group(2) if data.get(mm.group(1)) else ''
t=re.sub(r'[ \t]*\{\{#if (\w+)\}\}\n(.*?)[ \t]*\{\{/if\}\}\n',if_sub,t,flags=re.S)
def var_sub(mm):
    k=mm.group(1)
    if k not in data: raise SystemExit('missing var '+k)
    return esc(data[k])
t=re.sub(r'\{\{(\w+)\}\}',var_sub,t)
assert '{{' not in t, [l for l in t.splitlines() if '{{' in l][:3]
bom(f'{OUT}/installer.nsi', t)
print('rendered', f'{OUT}/installer.nsi', 'estimated_size_kb', size_kb)
env={k:v for k,v in os.environ.items() if k not in ('NSISDIR','NSISCONFDIR')}
subprocess.check_call(['makensis','-INPUTCHARSET','UTF8','-V2','installer.nsi'], cwd=OUT, env=env)
out=args.out or f'{ROOT}/dist/windows_amd64/portmasterpro_{version}_x64-setup.exe'
os.makedirs(os.path.dirname(out), exist_ok=True)
shutil.move(f'{OUT}/nsis-output.exe', out)
print('installer:', out, os.path.getsize(out), 'bytes')
