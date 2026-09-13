"""Release tags must select exactly the intended changelog body."""
import unittest
from release_notes import section, version_from_tag


class ReleaseNotesTests(unittest.TestCase):
    def test_initial_release(self):
        text = '# Changelog\n\n## [Unreleased]\n\n## [0.1.0] - 2026-09-13\n\nInitial release.\n\n## [0.0.1] - 2026-09-01\nOld.\n'
        self.assertEqual(section(text, version_from_tag('v0.1.0')), 'Initial release.\n')

    def test_prerelease(self):
        self.assertEqual(version_from_tag('v1.0.0-rc.1'), '1.0.0-rc.1')

    def test_invalid_tags(self):
        for tag in ('0.1.0', 'vv0.1.0', 'v2.0.0', 'v0.01.0', 'v0.1.0-rc.0', 'v0.1.0\n', 'v0.1.0;echo bad'):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                version_from_tag(tag)

    def test_invalid_sections(self):
        for text in ('## [Unreleased]\nText', '## [0.1.0]\nText', '## [0.1.0] - 2026-02-30\nText', '## [0.1.0] - 2026-09-13\n', '## [0.1.0] - 2026-09-13\nA\n## [0.1.0] - 2026-09-13\nB'):
            with self.subTest(text=text), self.assertRaises(ValueError):
                section(text, '0.1.0')


if __name__ == '__main__':
    unittest.main()
