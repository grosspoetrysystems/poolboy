#!/usr/bin/env python3

from __future__ import annotations

import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

from coverage_gate import aggregate, passes


def profile(*blocks: tuple[int, int]) -> str:
    lines = ["mode: atomic"]
    for i, (num_stmt, count) in enumerate(blocks):
        lines.append(f"pkg/f.go:{i}.1,{i}.9 {num_stmt} {count}")
    return "\n".join(lines) + "\n"


class CoverageGateTest(unittest.TestCase):
    def test_aggregate_counts_covered_statements(self) -> None:
        covered, total = aggregate(profile((5, 1), (5, 0), (3, 2)))
        self.assertEqual((covered, total), (8, 13))

    def test_aggregate_merges_duplicate_blocks(self) -> None:
        duplicate_profile = (
            "mode: atomic\n"
            "pkg/f.go:1.1,1.9 2 0\n"
            "pkg/f.go:1.1,1.9 2 1\n"
            "pkg/f.go:2.1,2.9 3 0\n"
        )

        self.assertEqual(aggregate(duplicate_profile), (2, 5))

    def test_exact_floor_passes(self) -> None:
        # 90/100 == exactly 90%.
        self.assertTrue(passes(90, 100))

    def test_just_below_floor_fails_without_rounding(self) -> None:
        # 89.9% must not round up to a pass.
        self.assertFalse(passes(899, 1000))

    def test_above_floor_passes(self) -> None:
        self.assertTrue(passes(91, 100))

    def test_empty_profile_is_error(self) -> None:
        with self.assertRaises(ValueError):
            aggregate("")

    def test_missing_mode_header_is_error(self) -> None:
        with self.assertRaises(ValueError):
            aggregate("pkg/f.go:1.1,1.9 2 1\n")

    def test_invalid_mode_is_error(self) -> None:
        with self.assertRaises(ValueError):
            aggregate("mode: invalid\npkg/f.go:1.1,1.9 1 1\n")

    def test_inconsistent_duplicate_statement_counts_are_errors(self) -> None:
        inconsistent = (
            "mode: atomic\n"
            "pkg/f.go:1.1,1.9 1 0\n"
            "pkg/f.go:1.1,1.9 2 0\n"
        )

        with self.assertRaisesRegex(ValueError, "inconsistent"):
            aggregate(inconsistent)

    def test_malformed_line_is_error(self) -> None:
        with self.assertRaises(ValueError):
            aggregate("mode: atomic\npkg/f.go garbage\n")

    def test_zero_statement_blocks_are_ignored_when_profile_has_statements(
        self,
    ) -> None:
        self.assertEqual(aggregate(profile((0, 22), (2, 1))), (2, 2))

    def test_zero_statements_is_error(self) -> None:
        with self.assertRaises(ValueError):
            aggregate(profile((0, 0)))


if __name__ == "__main__":
    unittest.main()
