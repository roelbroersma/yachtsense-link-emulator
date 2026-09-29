"""Regression tests for firmware-specific packaging without router access."""
from __future__ import annotations
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('builder', ROOT / 'scripts/build.py')
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)
PROFILE = json.loads((ROOT / 'tests/fixtures/rutx14-7.25.3.json').read_text())

class BuilderTests(unittest.TestCase):
    def test_actual_target_architecture(self):
        arch, fmt = builder.target_profile(PROFILE['firmware'])
        self.assertIn(arch, PROFILE['architectures'])
        self.assertEqual((arch, fmt), ('cortexa7hf-neon-vfpv4', 'ar'))

    def test_old_architecture_is_not_accepted_on_target(self):
        self.assertNotIn('arm_cortex-a7_neon-vfpv4', PROFILE['architectures'])

    def test_legacy_build_remains_distinct(self):
        self.assertEqual(builder.target_profile('RUTX_R_00.07.24.2'),
                         ('arm_cortex-a7_neon-vfpv4', 'tar'))

    def test_invalid_firmware_is_rejected(self):
        for name in ['7.25.3', 'RUTX_R_00.07.25.3;echo injected',
                     'RUTX_R_00.07.25', 'RUTM_R_00.07.25.3', '../bad']:
            with self.subTest(name=name), self.assertRaises(ValueError):
                builder.target_profile(name)

    def test_ar_round_trip_independent_reader(self):
        members = [('debian-binary', b'2.0\n'), ('control.tar.gz', b'odd'),
                   ('data.tar.gz', b'even')]
        raw = builder.ar_archive(members)
        with tempfile.TemporaryDirectory() as d:
            path = Path(d) / 'fixture.ipk'; path.write_bytes(raw)
            names = subprocess.check_output(['ar', 't', str(path)], text=True).splitlines()
            self.assertEqual(names, [name for name, _ in members])
            for name, data in members:
                self.assertEqual(subprocess.check_output(['ar', 'p', str(path), name]), data)

    def test_ar_is_deterministic(self):
        members = [('debian-binary', b'2.0\n'), ('data.tar.gz', b'data')]
        self.assertEqual(builder.ar_archive(members), builder.ar_archive(members))

    def test_ar_rejects_unsafe_names(self):
        for name in ['../data', '/data', 'a' * 16, 'data/name', 'data\nname']:
            with self.subTest(name=name), self.assertRaises(ValueError):
                builder.ar_archive([(name, b'data')])

    def test_tar_reproducible(self):
        data = {'main': (b'metadata', 0o644), 'a/b': (b'content', 0o755)}
        first = builder.archive(data)
        self.assertEqual(first, builder.archive(data))
        with tarfile.open(fileobj=io.BytesIO(first), mode='r:gz') as t:
            self.assertEqual(t.extractfile('./a/b').read(), b'content')
            self.assertEqual(t.getmember('./a/b').mode, 0o755)

if __name__ == '__main__':
    unittest.main(verbosity=2)
