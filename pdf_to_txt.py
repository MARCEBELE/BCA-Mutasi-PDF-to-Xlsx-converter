"""
BCA PDF to TXT converter
Converts BCA e-statement PDF to TXT format that parser.go can parse.
Usage: python pdf_to_txt.py input.pdf [output.txt]

Uses word-coordinate extraction to handle the BCA PDF table layout correctly.
Each PDF page has a 5-column table (TANGGAL | KETERANGAN | CBG | MUTASI | SALDO)
with fixed x-position boundaries. We reconstruct each logical line from
the column zones so the parser receives clean, well-structured input.
"""
import sys
import os
import re


# ── Column x-position boundaries (points) ────────────────────────────────
# Default fallback values determined by analysing word positions across
# multiple BCA PDF variants.  At runtime the actual boundaries are derived
# dynamically from the sentinel header row of each document so the converter
# keeps working even if BCA adjusts margins or font sizes in future PDFs.
_X_DATE_MAX_DEFAULT   = 82    # TANGGAL  : x0 < boundary
_X_MUTASI_MIN_DEFAULT = 370   # MUTASI   : x0 >= boundary
_X_SALDO_MIN_DEFAULT  = 490   # SALDO    : x0 >= boundary


def _detect_column_boundaries(sentinel_row_words):
    """
    Derive column x-boundaries from the sentinel header row word positions.

    Looks for the KETERANGAN, MUTASI, and SALDO words and returns their x0
    values as (x_date_max, x_mutasi_min, x_saldo_min).  The boundary between
    the date column and the description column is the left edge of KETERANGAN.

    Returns None if any of the three landmark words is missing (falls back to
    the hardcoded defaults in the caller).
    """
    col_x = {}
    for w in sentinel_row_words:
        t = w['text'].upper()
        if t == 'KETERANGAN':
            col_x['KETERANGAN'] = w['x0']
        elif t == 'MUTASI':
            col_x['MUTASI'] = w['x0']
        elif t == 'SALDO':
            col_x['SALDO'] = w['x0']
    if len(col_x) < 3:
        return None
    return col_x['KETERANGAN'], col_x['MUTASI'], col_x['SALDO']

# Keywords that mark the start and end of the transaction table
SENTINEL     = "TANGGAL KETERANGAN CBG MUTASI SALDO"
SUMMARY_STARTS = ("SALDO AWAL", "MUTASI CR", "MUTASI DB", "SALDO AKHIR")


def _extract_words(pdf_path):
    """Open PDF and return words with coordinates for every page."""
    try:
        import pdfplumber
    except ImportError:
        import subprocess
        subprocess.check_call([sys.executable, "-m", "pip", "install",
                               "pdfplumber", "--quiet"])
        import pdfplumber

    with pdfplumber.open(pdf_path) as pdf:
        for page in pdf.pages:
            words = page.extract_words(keep_blank_chars=False)
            yield words


def pdf_to_lines(pdf_path):
    """
    Extract transaction lines from a BCA e-statement PDF.

    Returns a list of strings in the same format produced by BCA's own
    PDF→TXT export, which is what parser.go expects:
      - One sentinel line per page: "TANGGAL KETERANGAN CBG MUTASI SALDO"
      - "Bersambung ke halaman berikut" between pages
      - Transaction lines: "DD/MM DESCRIPTION AMOUNT DB/CR BALANCE"
      - Continuation lines: "continuation text" (no date prefix)
      - Summary lines: "SALDO AWAL : ...", "MUTASI CR : ...", etc.
    """
    all_lines = []

    # Column boundaries – detected from the first sentinel row encountered.
    # Fall back to hardcoded defaults if detection fails (e.g. unusual PDF).
    x_date_max   = _X_DATE_MAX_DEFAULT
    x_mutasi_min = _X_MUTASI_MIN_DEFAULT
    x_saldo_min  = _X_SALDO_MIN_DEFAULT
    cols_detected = False
    first_page = True  # header info is only on the first page

    for page_words in _extract_words(pdf_path):
        if not page_words:
            continue

        # Group words by their vertical position (top), 2-point tolerance
        rows = {}
        for w in page_words:
            key = round(w['top'] / 2) * 2
            rows.setdefault(key, []).append(w)

        sorted_keys = sorted(rows.keys())

        # ── First-page header extraction ──────────────────────────────────
        # Scan pre-sentinel rows and emit the lines that carry account info.
        # BCA places account holder name, account number, period, and currency
        # in the header block before the column-header sentinel.  We need these
        # lines in the TXT output so parser.go's regex-based extractAccountInfo
        # can find them.  Only the three rows that actually contain the key
        # fields are emitted; all other header rows (address, disclaimer, …)
        # are discarded.
        if first_page:
            for top_key in sorted_keys:
                row_words = sorted(rows[top_key], key=lambda w: w['x0'])
                texts = [w['text'] for w in row_words]
                full_text = ' '.join(texts)
                if 'TANGGAL' in texts and 'SALDO' in texts and 'CBG' in texts:
                    break  # reached the sentinel — stop header scan
                if any(kw in full_text for kw in ('NO. REKENING', 'PERIODE :', 'MATA UANG')):
                    all_lines.append(full_text)
            first_page = False

        in_table = False
        page_lines = []

        for top_key in sorted_keys:
            row_words = sorted(rows[top_key], key=lambda w: w['x0'])
            texts = [w['text'] for w in row_words]
            full_text = ' '.join(texts)

            # ── Detect column-header sentinel ──────────────────────────
            if ('TANGGAL' in texts and 'SALDO' in texts and 'CBG' in texts):
                in_table = True
                page_lines.append(SENTINEL)
                # Derive column boundaries from this header row once.
                # Subsequent pages reuse the same boundaries (consistent template).
                if not cols_detected:
                    detected = _detect_column_boundaries(row_words)
                    if detected:
                        x_date_max, x_mutasi_min, x_saldo_min = detected
                        cols_detected = True
                continue

            # ── Detect page-break footer ────────────────────────────────
            if 'Bersambung' in full_text:
                if in_table:
                    page_lines.append("Bersambung ke halaman berikut")
                continue

            if not in_table:
                continue

            # ── Summary footer lines ────────────────────────────────────
            if any(full_text.startswith(s) for s in SUMMARY_STARTS):
                page_lines.append(full_text)
                continue

            # ── Skip page-number lines like "1 / 4" ─────────────────────
            if re.match(r'^\d+\s*/\s*\d+$', full_text.strip()):
                continue

            # ── Classify words into column zones ────────────────────────
            # TANGGAL column (date): x0 < x_date_max
            date_words = [w for w in row_words if w['x0'] < x_date_max]

            # KETERANGAN+CBG columns (description): x_date_max ≤ x0 < x_mutasi_min
            # We intentionally keep DB/CR keywords here so detect_type() in the
            # parser can find them on the primary date line (buf[0]).
            desc_words = [w for w in row_words
                          if x_date_max <= w['x0'] < x_mutasi_min]

            # MUTASI column — comma-formatted numbers only
            mutasi_words = [w for w in row_words
                            if w['x0'] >= x_mutasi_min and w['x0'] < x_saldo_min
                            and re.match(r'[\d,]+\.\d{2}', w['text'])]

            # DB/CR type marker (separate so we can append it cleanly)
            dbcr_words = [w for w in row_words
                          if w['x0'] >= x_mutasi_min and w['x0'] < x_saldo_min
                          and w['text'] in ('DB', 'CR')]

            # SALDO column
            saldo_words = [w for w in row_words if w['x0'] >= x_saldo_min]

            date_str   = ' '.join(w['text'] for w in date_words)
            desc_str   = ' '.join(w['text'] for w in desc_words)
            mutasi_str = ' '.join(w['text'] for w in mutasi_words)
            dbcr_str   = ' '.join(w['text'] for w in dbcr_words)
            saldo_str  = ' '.join(w['text'] for w in saldo_words)

            # ── Reconstruct output line(s) ──────────────────────────────
            if date_str:
                # Primary transaction line
                parts = [date_str]
                if desc_str:    parts.append(desc_str)
                if mutasi_str:  parts.append(mutasi_str)
                if dbcr_str:    parts.append(dbcr_str)
                if saldo_str:   parts.append(saldo_str)
                page_lines.append(' '.join(parts))
            else:
                # Continuation line (no date prefix)
                parts = []
                if desc_str:    parts.append(desc_str)
                if mutasi_str:  parts.append(mutasi_str)
                if dbcr_str:    parts.append(dbcr_str)
                if saldo_str:   parts.append(saldo_str)
                if parts:
                    page_lines.append(' '.join(parts))

        all_lines.extend(page_lines)

    return all_lines


def convert(pdf_path, txt_path=None):
    """Convert BCA PDF to TXT file. Returns the output path."""
    if txt_path is None:
        base = os.path.splitext(pdf_path)[0]
        txt_path = base + ".txt"

    lines = pdf_to_lines(pdf_path)

    with open(txt_path, "w", encoding="utf-8") as f:
        f.write("\n".join(lines))

    return txt_path


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python pdf_to_txt.py input.pdf [output.txt]")
        sys.exit(1)

    pdf_path = sys.argv[1]
    txt_path = sys.argv[2] if len(sys.argv) > 2 else None

    result = convert(pdf_path, txt_path)
    print(result)  # stdout → Go reads this path