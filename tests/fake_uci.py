#!/usr/bin/python3
"""Small UCI fixture for transaction tests; not a replacement for native RutOS UCI."""
import pathlib, shlex, sys
args = sys.argv[1:]
root = pathlib.Path(args[args.index('-c') + 1])
path = root / 'yachtsense_link_emulator'
values = {}
active = False
for line in path.read_text().splitlines():
    w = shlex.split(line, comments=True)
    if not w:
        continue
    if w[0] == 'config':
        active = len(w) > 2 and w[1:3] == ['emulator', 'main']
    elif active and len(w) == 3 and w[0] in ('option', 'list'):
        values[w[1]] = ([w[2]] if w[0] == 'option' else values.get(w[1], []) + [w[2]])
for line in sys.stdin:
    w = shlex.split(line)
    if not w:
        continue
    if w[0] == 'commit':
        continue
    key, _, val = w[1].partition('=')
    if key == 'yachtsense_link_emulator.main':
        continue
    key = key.split('.')[-1]
    if w[0] == 'delete':
        values.pop(key, None)
    elif w[0] == 'set':
        values[key] = [val]
    elif w[0] == 'add_list':
        values.setdefault(key, []).append(val)
    else:
        raise ValueError('Unexpected UCI operation')
lines = ["config emulator 'main'"]
for key, entries in values.items():
    for value in entries:
        kind = 'list' if key == 'remote_interface' else 'option'
        lines.append(f' {kind} {key} {shlex.quote(value)}')
path.write_text('\n'.join(lines) + '\n')
