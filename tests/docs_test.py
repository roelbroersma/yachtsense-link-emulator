"""Check current guides, local links and versioned install references."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
VERSION = (ROOT / 'VERSION').read_text().strip()
readme = (ROOT / 'README.md').read_text()
assert readme.startswith('# YachtSense Link Emulator\n')
assert f'Current release: {VERSION}' in readme
assert f'yachtsense-link-emulator_{VERSION}-1_RUTX_00.07.25.3.tar.gz' in readme
assert f'/releases/tag/v{VERSION}' in readme
assert 'Ethernet / RayNet' in readme and 'Raymarine app discovery' in readme
assert '8088' in readme and '7777' in readme
assert 'home VPN has not yet been\nconfirmed' in readme
assert 'relay' in (ROOT / 'docs/networking.md').read_text()
assert '**RayNet has one Services row**' in (ROOT / 'docs/public-status.md').read_text()

count = 0
for file in [*ROOT.glob('*.md'), *sorted((ROOT / 'docs').rglob('*.md'))]:
    content = file.read_text()
    prose = re.sub(r'^```.*?^```[^\n]*$', '', content, flags=re.M | re.S)
    expected = 0 if file.name == 'RELEASE.md' else 1
    assert len(re.findall(r'^# ', prose, flags=re.M)) == expected, file
    assert len(re.findall(r'^```', content, flags=re.M)) % 2 == 0, file
    for target in re.findall(r'\]\(([^)]+)\)', content):
        if re.match(r'^[a-zA-Z][a-zA-Z0-9+.-]*:', target) or target.startswith('#'):
            continue
        path = target.split('#', 1)[0]
        if path:
            assert (file.parent / path).exists(), (file, target)
    count += 1
print(f'{count} Markdown guides checked; current version, purpose and local links consistent')
