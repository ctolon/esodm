"""Exercise draft publication decisions without contacting GitHub."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().with_name('draft_release.sh')
FAKE_GH = '''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
with Path('calls.jsonl').open('a') as out:
    out.write(json.dumps(sys.argv[1:]) + '\\n')
if sys.argv[1:3] == ['release', 'view']:
    mode = os.environ['RELEASE_TEST_MODE']
    if mode == 'missing':
        sys.exit(1)
    print(json.dumps({'isDraft': mode == 'draft'}))
'''


class DraftReleaseTests(unittest.TestCase):
    def run_draft(self, mode, tag='v0.1.0'):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            gh = root / 'gh'
            gh.write_text(FAKE_GH)
            gh.chmod(0o755)
            env = dict(os.environ, PATH=directory + os.pathsep + os.environ['PATH'],
                       RELEASE_TAG=tag, GH_REPO='test/repo', GH_TOKEN='test-only', RELEASE_TEST_MODE=mode)
            result = subprocess.run(['bash', str(SCRIPT)], cwd=root, env=env, capture_output=True, text=True)
            calls = [json.loads(line) for line in (root / 'calls.jsonl').read_text().splitlines()]
            return result, calls

    def test_create_draft(self):
        result, calls = self.run_draft('missing')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([c[1] for c in calls], ['view', 'create'])
        self.assertIn('--draft', calls[1])
        self.assertIn('--verify-tag', calls[1])
        self.assertIn('--prerelease=false', calls[1])
        self.assertIn('SHA256SUMS', calls[1])

    def test_update_existing_draft(self):
        result, calls = self.run_draft('draft', 'v0.1.0-rc.1')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([c[1] for c in calls], ['view', 'edit', 'upload'])
        self.assertIn('--prerelease', calls[1])
        self.assertIn('--clobber', calls[2])

    def test_published_release_is_untouched(self):
        result, calls = self.run_draft('published')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual([c[1] for c in calls], ['view'])
        self.assertIn('Refusing to change a published release', result.stderr)


if __name__ == '__main__':
    unittest.main()
