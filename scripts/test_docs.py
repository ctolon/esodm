"""Regression tests for documentation contract checks; no network required."""
from pathlib import Path
import tempfile
import unittest

from docs import anchors, local_link_error


class DocumentationTests(unittest.TestCase):
    def test_heading_anchors(self):
        text = '# API\n## `ReadOptions.Source`\n## Repeat\n## Repeat\n```go\n# hidden\n```\n'
        self.assertEqual(anchors(text), {'api', 'readoptionssource', 'repeat', 'repeat-1'})

    def test_missing_and_encoded_anchors(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'guide.md'
            path.write_text('# Guide\n## Source fields\n')
            self.assertIsNone(local_link_error(path, '#source-fields'))
            self.assertIsNone(local_link_error(path, 'guide.md#source%2Dfields'))
            self.assertIn('missing anchor', local_link_error(path, '#absent'))
            self.assertIn('broken link', local_link_error(path, 'missing.md'))
            self.assertIsNone(local_link_error(path, 'https://example.org/#external'))


if __name__ == '__main__':
    unittest.main()
