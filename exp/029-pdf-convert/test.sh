#!/bin/bash
# exp-029: PDF conversion pipeline end-to-end tests.
# Tests:
#   - scripts/pdf2md: single-file conversion (pdftotext → ollama → .pdf.md)
#   - scripts/pdf2md: stub generation for image-only PDFs
#   - scripts/pdf2md: --batch mode
#   - scripts/pdf2md-enqueue: queue mechanics (symlink, flock, runner)
#   - Server auto-enqueue via pdf_convert_cmd config
#
# Requires: pdftotext, flock, ollama (local, any small model)
set -u

PASS=0; FAIL=0
assert() {
  local label="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    echo "  ✓ $label"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $label"
    echo "    expected: $expected"
    echo "    actual:   $actual"
    FAIL=$((FAIL + 1))
  fi
}
assert_contains() {
  local label="$1" needle="$2" haystack="$3"
  if echo "$haystack" | grep -qF "$needle"; then
    echo "  ✓ $label"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $label"
    echo "    expected to contain: $needle"
    echo "    actual: ${haystack:0:200}"
    FAIL=$((FAIL + 1))
  fi
}

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# --- Create a minimal valid PDF with extractable text ---
make_text_pdf() {
  local path="$1" text="$2"
  cat > "$path" << PDFEOF
%PDF-1.0
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj
4 0 obj << /Length $((${#text} + 39)) >>
stream
BT /F1 12 Tf 100 700 Td ($text) Tj ET
endstream
endobj
5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj
xref
0 6
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000266 00000 n 
0000000360 00000 n 
trailer << /Size 6 /Root 1 0 R >>
startxref
441
%%EOF
PDFEOF
}

# --- Create an empty PDF (no extractable text, triggers stub) ---
make_empty_pdf() {
  local path="$1"
  cat > "$path" << 'PDFEOF'
%PDF-1.0
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >> endobj
xref
0 4
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
trailer << /Size 4 /Root 1 0 R >>
startxref
174
%%EOF
PDFEOF
}

# Detect a local ollama model for conversion tests.
OLLAMA_MODEL=$(ollama list 2>/dev/null | awk 'NR==2 {print $1}')
if [ -z "$OLLAMA_MODEL" ]; then
  echo "SKIP: no ollama model available locally"
  exit 0
fi
echo "Using ollama model: $OLLAMA_MODEL"

echo ""
echo "=== 1. pdf2md: single file conversion ==="
make_text_pdf "$TMPDIR/report.pdf" "Quarterly revenue increased by twelve percent"
"$REPO/scripts/pdf2md" "$TMPDIR/report.pdf" --model "$OLLAMA_MODEL" 2>&1
assert "shadow file created" "true" "$([ -f "$TMPDIR/report.pdf.md" ] && echo true || echo false)"
SHADOW=$(cat "$TMPDIR/report.pdf.md")
assert_contains "shadow has revenue content" "revenue" "$SHADOW"

echo ""
echo "=== 2. pdf2md: skip existing (no --force) ==="
BEFORE=$(cat "$TMPDIR/report.pdf.md")
"$REPO/scripts/pdf2md" "$TMPDIR/report.pdf" --model "$OLLAMA_MODEL" 2>&1
AFTER=$(cat "$TMPDIR/report.pdf.md")
assert "shadow unchanged without --force" "$BEFORE" "$AFTER"

echo ""
echo "=== 3. pdf2md: --force overwrites ==="
echo "old content" > "$TMPDIR/report.pdf.md"
"$REPO/scripts/pdf2md" "$TMPDIR/report.pdf" --model "$OLLAMA_MODEL" --force 2>&1
SHADOW=$(cat "$TMPDIR/report.pdf.md")
assert_contains "shadow refreshed with --force" "revenue" "$SHADOW"

echo ""
echo "=== 4. pdf2md: stub for image-only PDF ==="
make_empty_pdf "$TMPDIR/scan.pdf"
"$REPO/scripts/pdf2md" "$TMPDIR/scan.pdf" --model "$OLLAMA_MODEL" 2>&1
assert "stub file created" "true" "$([ -f "$TMPDIR/scan.pdf.md" ] && echo true || echo false)"
STUB=$(cat "$TMPDIR/scan.pdf.md")
assert_contains "stub has image-only marker" "Image-only PDF" "$STUB"
assert_contains "stub has file size" "File size" "$STUB"

echo ""
echo "=== 5. pdf2md: --batch mode ==="
BATCH_DIR="$TMPDIR/batch"
mkdir -p "$BATCH_DIR/notes"
make_text_pdf "$BATCH_DIR/notes/alpha.pdf" "Alpha document content"
make_text_pdf "$BATCH_DIR/notes/beta.pdf" "Beta document content"
# Pre-create a shadow for beta to verify skip.
echo "existing" > "$BATCH_DIR/notes/beta.pdf.md"
OUTPUT=$("$REPO/scripts/pdf2md" --batch "$BATCH_DIR" --model "$OLLAMA_MODEL" 2>&1)
assert "alpha shadow created" "true" "$([ -f "$BATCH_DIR/notes/alpha.pdf.md" ] && echo true || echo false)"
ALPHA=$(cat "$BATCH_DIR/notes/alpha.pdf.md")
assert_contains "alpha has content" "Alpha" "$ALPHA"
BETA=$(cat "$BATCH_DIR/notes/beta.pdf.md")
assert "beta unchanged (skipped)" "existing" "$BETA"
assert_contains "batch found 1 unconverted" "1 unconverted" "$OUTPUT"

echo ""
echo "=== 6. pdf2md: --batch --dry-run ==="
make_text_pdf "$BATCH_DIR/notes/gamma.pdf" "Gamma document"
OUTPUT=$("$REPO/scripts/pdf2md" --batch "$BATCH_DIR" --model "$OLLAMA_MODEL" --dry-run 2>&1)
assert "gamma NOT converted in dry-run" "false" "$([ -f "$BATCH_DIR/notes/gamma.pdf.md" ] && echo true || echo false)"
assert_contains "dry-run lists gamma" "gamma.pdf" "$OUTPUT"

echo ""
echo "=== 7. pdf2md-enqueue: queue mechanics ==="
QUEUE_DIR="$TMPDIR/queue-test/.pdf-queue"
mkdir -p "$TMPDIR/queue-test"
make_text_pdf "$TMPDIR/queue-test/enq.pdf" "Enqueued document about bridges"
# Run enqueue — it should symlink, get the lock, convert, remove link.
"$REPO/scripts/pdf2md-enqueue" "$TMPDIR/queue-test/enq.pdf" "$QUEUE_DIR" --model "$OLLAMA_MODEL" 2>&1
# Wait for the backgrounded runner to finish.
sleep 5
# Check for flock — try to acquire the lock ourselves.
if flock -n "$QUEUE_DIR/.pid" true 2>/dev/null; then
  echo "  ✓ lock released after runner finished"
  PASS=$((PASS + 1))
else
  echo "  ✗ lock still held (runner may still be running, waiting...)"
  sleep 30
  FAIL=$((FAIL + 1))
fi
assert "shadow created by enqueue" "true" "$([ -f "$TMPDIR/queue-test/enq.pdf.md" ] && echo true || echo false)"
# Queue link should be cleaned up.
LINKS=$(find "$QUEUE_DIR" -name '*enq*' 2>/dev/null | wc -l | tr -d ' ')
assert "queue link removed" "0" "$LINKS"

echo ""
echo "=== 8. pdf2md-enqueue: flock prevents double runner ==="
QUEUE2="$TMPDIR/flock-test/.pdf-queue"
mkdir -p "$QUEUE2"
mkdir -p "$TMPDIR/flock-test"
# Hold the lock ourselves.
exec 8>"$QUEUE2/.pid"
flock -n 8
make_text_pdf "$TMPDIR/flock-test/blocked.pdf" "Blocked content"
"$REPO/scripts/pdf2md-enqueue" "$TMPDIR/flock-test/blocked.pdf" "$QUEUE2" --model "$OLLAMA_MODEL" 2>&1
# The enqueue should have exited without converting (lock held by us).
assert "no shadow when locked" "false" "$([ -f "$TMPDIR/flock-test/blocked.pdf.md" ] && echo true || echo false)"
# But the symlink should be in the queue.
LINKS=$(find "$QUEUE2" -type l 2>/dev/null | wc -l | tr -d ' ')
assert "symlink queued" "1" "$LINKS"
# Release our lock.
exec 8>&-

echo ""
echo "=== 9. Server auto-enqueue on GET ==="
WIKI_DIR="$TMPDIR/autowiki"
mkdir -p "$WIKI_DIR"
cat > "$WIKI_DIR/PdfNote.md" <<'EOF'
title: PdfNote
tags: Test
modified: 20260329000000000

A note with an unconverted PDF.
EOF
mkdir -p "$WIKI_DIR/PdfNote"
make_text_pdf "$WIKI_DIR/PdfNote/invoice.pdf" "Invoice total three hundred dollars"

cat > "$TMPDIR/auto-config.yaml" <<EOF
wiki_path: .
listen: 127.0.0.1:8091
pdf_convert_cmd: $REPO/scripts/pdf2md-enqueue
cache_size: 100
max_diffs: 10
EOF

echo "Building..."
cd "$REPO"
go build -o "$TMPDIR/tibbitwiki" .

echo "Starting server on :8091..."
"$TMPDIR/tibbitwiki" --config "$TMPDIR/auto-config.yaml" &
SERVER_PID=$!
trap "kill $SERVER_PID 2>/dev/null; rm -rf $TMPDIR" EXIT
sleep 2

# GET the note — should trigger auto-enqueue of invoice.pdf
curl -s "http://127.0.0.1:8091/autowiki/notes/PdfNote" > /dev/null

# Wait for the backgrounded conversion to complete.
echo "  waiting for background conversion..."
for i in $(seq 1 30); do
  if [ -f "$WIKI_DIR/PdfNote/invoice.pdf.md" ]; then
    break
  fi
  sleep 2
done
assert "auto-convert created shadow" "true" "$([ -f "$WIKI_DIR/PdfNote/invoice.pdf.md" ] && echo true || echo false)"
if [ -f "$WIKI_DIR/PdfNote/invoice.pdf.md" ]; then
  SHADOW=$(cat "$WIKI_DIR/PdfNote/invoice.pdf.md")
  assert_contains "shadow has invoice content" "invoice" "$(echo "$SHADOW" | tr '[:upper:]' '[:lower:]')"
fi

# Second GET should NOT re-enqueue (shadow exists now).
QUEUE_DIR="$WIKI_DIR/.pdf-queue"
rm -f "$QUEUE_DIR"/* 2>/dev/null
curl -s "http://127.0.0.1:8091/autowiki/notes/PdfNote" > /dev/null
sleep 1
LINKS=$(find "$QUEUE_DIR" -type l 2>/dev/null | wc -l | tr -d ' ')
assert "no re-enqueue after conversion" "0" "$LINKS"

kill $SERVER_PID 2>/dev/null
wait $SERVER_PID 2>/dev/null

echo ""
echo "================================"
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && echo "ALL TESTS PASSED" || echo "SOME TESTS FAILED"
exit "$FAIL"
