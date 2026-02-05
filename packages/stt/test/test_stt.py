"""
STT Service Test Script

Usage:
  python test_stt.py              # Run all tests from manifest.json
  python test_stt.py --health     # Only check service health
  python test_stt.py file.wav     # Test a specific file (no validation)

Run from packages/stt/test with the venv activated:
  cd packages/stt
  venv\\Scripts\\activate
  cd test
  python test_stt.py

Test cases are defined in media/manifest.json
"""

import sys
import json
import httpx
import re
from pathlib import Path
from dataclasses import dataclass

STT_URL = "http://localhost:51741"
MEDIA_DIR = Path(__file__).parent / "media"
MANIFEST_PATH = MEDIA_DIR / "manifest.json"


@dataclass
class TestResult:
    file: str
    description: str
    expected: str
    actual: str
    similarity: float
    passed: bool
    error: str | None = None


def normalize_text(text: str) -> str:
    """Normalize text for comparison"""
    text = text.lower().strip()
    text = re.sub(r'[^\w\s]', '', text)  # Remove punctuation
    text = re.sub(r'\s+', ' ', text)      # Normalize whitespace
    return text


def calculate_similarity(expected: str, actual: str) -> float:
    """Calculate word-level similarity between expected and actual text"""
    expected_words = set(normalize_text(expected).split())
    actual_words = set(normalize_text(actual).split())

    if not expected_words:
        return 1.0 if not actual_words else 0.0

    intersection = expected_words & actual_words
    union = expected_words | actual_words

    return len(intersection) / len(union) if union else 0.0


def test_health() -> bool:
    """Test health endpoint"""
    print("Testing /health...")
    try:
        resp = httpx.get(f"{STT_URL}/health")
        print(f"  Status: {resp.status_code}")
        print(f"  Response: {resp.json()}")
        return resp.status_code == 200
    except Exception as e:
        print(f"  Error: {e}")
        return False


def transcribe_file(audio_path: Path) -> tuple[str | None, str | None]:
    """Transcribe an audio file, returns (text, error)"""
    try:
        with open(audio_path, 'rb') as f:
            files = {'file': (audio_path.name, f.read())}
            resp = httpx.post(f"{STT_URL}/transcribe", files=files, timeout=120)

        if resp.status_code == 200:
            return resp.json().get('text', ''), None
        else:
            return None, f"HTTP {resp.status_code}: {resp.text}"
    except Exception as e:
        return None, str(e)


def run_test(test_case: dict, threshold: float = 0.7) -> TestResult:
    """Run a single test case"""
    file_path = MEDIA_DIR / test_case['file']
    expected = test_case['expected']
    description = test_case.get('description', '')

    if not file_path.exists():
        return TestResult(
            file=test_case['file'],
            description=description,
            expected=expected,
            actual='',
            similarity=0.0,
            passed=False,
            error=f"File not found: {file_path}"
        )

    actual, error = transcribe_file(file_path)

    if error:
        return TestResult(
            file=test_case['file'],
            description=description,
            expected=expected,
            actual='',
            similarity=0.0,
            passed=False,
            error=error
        )

    similarity = calculate_similarity(expected, actual)
    passed = similarity >= threshold

    return TestResult(
        file=test_case['file'],
        description=description,
        expected=expected,
        actual=actual,
        similarity=similarity,
        passed=passed
    )


def load_manifest() -> list[dict]:
    """Load test manifest"""
    if not MANIFEST_PATH.exists():
        return []
    with open(MANIFEST_PATH) as f:
        data = json.load(f)
    return data.get('tests', [])


def print_result(result: TestResult, verbose: bool = True):
    """Print a test result"""
    status = "PASS" if result.passed else "FAIL"
    print(f"\n{status} - {result.file}")

    if result.description:
        print(f"  Description: {result.description}")

    if result.error:
        print(f"  Error: {result.error}")
    else:
        print(f"  Similarity: {result.similarity:.1%}")
        if verbose or not result.passed:
            print(f"  Expected: {result.expected}")
            print(f"  Actual:   {result.actual}")


def main():
    print("=" * 60)
    print("STT Service Test Suite")
    print("=" * 60)

    # Health check
    if not test_health():
        print("\nService not running! Start it with:")
        print("  cd packages/stt")
        print("  venv\\Scripts\\activate")
        print("  uvicorn main:app --host 0.0.0.0 --port 51741")
        sys.exit(1)

    # Handle --health flag
    if len(sys.argv) > 1 and sys.argv[1] == '--health':
        print("\nHealth check passed!")
        return

    # Handle specific file argument
    if len(sys.argv) > 1 and sys.argv[1] != '--health':
        file_path = Path(sys.argv[1])
        if not file_path.is_absolute():
            file_path = MEDIA_DIR / file_path

        print(f"\nTranscribing: {file_path}")
        text, error = transcribe_file(file_path)
        if error:
            print(f"  Error: {error}")
        else:
            print(f"  Result: {text}")
        return

    # Run all tests from manifest
    tests = load_manifest()
    if not tests:
        print(f"\nNo tests found in {MANIFEST_PATH}")
        print("Add test cases to media/manifest.json")
        return

    print(f"\nRunning {len(tests)} test(s)...")

    results = []
    for test_case in tests:
        result = run_test(test_case)
        results.append(result)
        print_result(result)

    # Summary
    passed = sum(1 for r in results if r.passed)
    failed = len(results) - passed

    print("\n" + "=" * 60)
    print(f"Results: {passed} passed, {failed} failed, {len(results)} total")

    if failed > 0:
        sys.exit(1)


if __name__ == "__main__":
    main()
